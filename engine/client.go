// Package engine is a small, dependency-free client for the Docker Engine API.
//
// It speaks the Engine's HTTP API directly (over a unix socket or TCP/TLS) and
// exposes typed operations for containers, images, volumes, networks, exec
// sessions and system information. Wire types mirror the Engine API field
// names so responses decode without translation; presentation belongs to the
// layers above.
package engine

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// MinAPIVersion is the oldest Engine API this client supports (Docker 25).
	MinAPIVersion = "1.44"
	// MaxAPIVersion is the newest Engine API this client has been verified
	// against. Newer daemons are spoken to at this version.
	MaxAPIVersion = "1.56"

	DefaultHost = "unix:///var/run/docker.sock"
)

// Config describes how to reach a Docker daemon.
type Config struct {
	// Host is a daemon address such as unix:///var/run/docker.sock or
	// tcp://10.0.0.2:2376.
	Host string
	// TLS enables HTTPS for tcp:// hosts.
	TLS *tls.Config
	// APIVersion pins the API version. Empty negotiates with the daemon.
	APIVersion string
}

// Client is safe for concurrent use.
type Client struct {
	host   string
	scheme string // http or https
	addr   string // dial address (socket path or host:port)
	tls    *tls.Config
	http   *http.Client

	versionMu sync.Mutex
	version   string
}

// New creates a client for the daemon described by cfg.
func New(cfg Config) (*Client, error) {
	host := cfg.Host
	if host == "" {
		host = DefaultHost
	}
	u, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("engine: invalid host %q: %w", host, err)
	}
	c := &Client{host: host, tls: cfg.TLS, version: cfg.APIVersion}
	switch u.Scheme {
	case "unix":
		c.scheme, c.addr = "http", u.Path
	case "tcp", "http", "https":
		c.scheme, c.addr = "http", u.Host
		if cfg.TLS != nil || u.Scheme == "https" {
			c.scheme = "https"
		}
		if u.Port() == "" {
			port := "2375"
			if c.scheme == "https" {
				port = "2376"
			}
			c.addr = net.JoinHostPort(u.Hostname(), port)
		}
	default:
		return nil, fmt.Errorf("engine: unsupported host scheme %q", u.Scheme)
	}
	network := "tcp"
	if u.Scheme == "unix" {
		network = "unix"
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, c.addr)
		},
		TLSClientConfig:     cfg.TLS,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     90 * time.Second,
	}
	c.http = &http.Client{Transport: transport}
	return c, nil
}

// Host returns the daemon address the client was configured with.
func (c *Client) Host() string { return c.host }

// Close releases idle connections.
func (c *Client) Close() error {
	c.http.CloseIdleConnections()
	return nil
}

// APIVersion returns the negotiated API version, negotiating on first use.
func (c *Client) APIVersion(ctx context.Context) (string, error) {
	c.versionMu.Lock()
	defer c.versionMu.Unlock()
	if c.version != "" {
		return c.version, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.scheme+"://docker/_ping", nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("engine: %w at %s: %w", ErrUnavailable, c.host, err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	server := resp.Header.Get("API-Version")
	if server == "" {
		return "", fmt.Errorf("engine: daemon at %s did not report an API version", c.host)
	}
	if compareVersions(server, MinAPIVersion) < 0 {
		return "", fmt.Errorf("engine: daemon API %s is older than the minimum supported %s", server, MinAPIVersion)
	}
	c.version = server
	if compareVersions(server, MaxAPIVersion) > 0 {
		c.version = MaxAPIVersion
	}
	return c.version, nil
}

// compareVersions compares dotted numeric versions such as "1.44".
func compareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			y, _ = strconv.Atoi(bs[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// ErrUnavailable wraps failures to reach the daemon at all (as opposed to
// errors the daemon reports), so callers can tell "Docker is down" apart.
var ErrUnavailable = errors.New("docker daemon unavailable")

// NotInstalled reports whether err, a failure to reach the daemon, is because
// Docker is not installed here at all: its socket does not exist and neither
// docker nor dockerd is on PATH.
func NotInstalled(err error) bool {
	if !errors.Is(err, ErrUnavailable) || !errors.Is(err, fs.ErrNotExist) {
		return false
	}
	for _, name := range []string{"dockerd", "docker"} {
		if _, err := exec.LookPath(name); err == nil {
			return false
		}
	}
	return true
}

// Error is a non-2xx response from the daemon.
type Error struct {
	StatusCode int
	Message    string
}

func (e *Error) Error() string { return e.Message }

// IsNotFound reports whether err is a daemon 404.
func IsNotFound(err error) bool { return statusIs(err, http.StatusNotFound) }

// IsConflict reports whether err is a daemon 409 (e.g. resource in use).
func IsConflict(err error) bool { return statusIs(err, http.StatusConflict) }

func statusIs(err error, code int) bool {
	var e *Error
	return errors.As(err, &e) && e.StatusCode == code
}

// request is one Engine API call.
type request struct {
	method string
	path   string
	query  url.Values
	body   io.Reader
	json   any // marshalled as the body when set
	header http.Header
}

func (c *Client) url(ctx context.Context, path string, query url.Values) (string, error) {
	version, err := c.APIVersion(ctx)
	if err != nil {
		return "", err
	}
	u := c.scheme + "://docker/v" + version + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u, nil
}

// do sends r and returns the response for a 2xx (or 304) status. The caller
// owns the body. Error statuses are decoded into *Error.
func (c *Client) do(ctx context.Context, r request) (*http.Response, error) {
	u, err := c.url(ctx, r.path, r.query)
	if err != nil {
		return nil, err
	}
	body := r.body
	if r.json != nil {
		data, err := json.Marshal(r.json)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, u, body)
	if err != nil {
		return nil, err
	}
	maps.Copy(req.Header, r.header)
	if r.json != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, fmt.Errorf("engine: %s %s: %w: %w", r.method, r.path, ErrUnavailable, err)
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close()
		return nil, decodeError(resp)
	}
	return resp, nil
}

func decodeError(resp *http.Response) error {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	var payload struct {
		Message string `json:"message"`
	}
	message := strings.TrimSpace(string(data))
	if json.Unmarshal(data, &payload) == nil && payload.Message != "" {
		message = payload.Message
	}
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	return &Error{StatusCode: resp.StatusCode, Message: message}
}

// call sends r, decodes a JSON response into out (when non-nil) and closes
// the body.
func (c *Client) call(ctx context.Context, r request, out any) error {
	resp, err := c.do(ctx, r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil || resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotModified {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// HijackedConn is a raw, bidirectional connection taken over from an HTTP
// upgrade, used for interactive exec sessions.
type HijackedConn struct {
	net.Conn
	reader *bufio.Reader
}

// Read reads from the buffered connection so bytes received alongside the
// upgrade response are not lost.
func (h *HijackedConn) Read(p []byte) (int, error) { return h.reader.Read(p) }

// CloseWrite half-closes the connection, signalling EOF on stdin.
func (h *HijackedConn) CloseWrite() error {
	if cw, ok := h.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}

// hijack performs an HTTP upgrade to a raw TCP stream.
func (c *Client) hijack(ctx context.Context, path string, payload any) (*HijackedConn, error) {
	u, err := c.url(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	network := "tcp"
	if strings.HasPrefix(c.host, "unix://") {
		network = "unix"
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, network, c.addr)
	if err != nil {
		return nil, fmt.Errorf("engine: %w at %s: %w", ErrUnavailable, c.host, err)
	}
	if c.scheme == "https" {
		cfg := c.tls.Clone()
		if cfg.ServerName == "" {
			cfg.ServerName, _, _ = net.SplitHostPort(c.addr)
		}
		tlsConn := tls.Client(conn, cfg)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, err
		}
		conn = tlsConn
	}
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(data))
	if err != nil {
		conn.Close()
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "tcp")
	if err := req.Write(conn); err != nil {
		conn.Close()
		return nil, err
	}
	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, req)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols && resp.StatusCode != http.StatusOK {
		defer conn.Close()
		return nil, decodeError(resp)
	}
	return &HijackedConn{Conn: conn, reader: reader}, nil
}

// Filters is the Engine API filter map, e.g. {"label": ["a=b"]}.
type Filters map[string][]string

func (f Filters) encode(q url.Values) {
	if len(f) == 0 {
		return
	}
	data, _ := json.Marshal(f)
	q.Set("filters", string(data))
}

func boolString(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
