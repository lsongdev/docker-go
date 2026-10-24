// Command docker-go serves the Docker management panel on its own. A panel
// shell can mount the same module (web.New) beside others instead.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lsongdev/docker-go/web"
)

const usage = `usage: docker-go [flags]

Serves the Docker panel. Set DOCKER_GO_PASSWORD to require HTTP basic
authentication (any user name).

Flags:
`

func main() {
	flag.Usage = func() { fmt.Fprint(flag.CommandLine.Output(), usage); flag.PrintDefaults() }
	listen := flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
	dataDir := flag.String("data", "./data", "directory for stacks, apps and the catalog")
	catalogRepo := flag.String("catalog", web.DefaultCatalog, `app catalog git repository ("-": use <data>/catalog as is)`)
	host := flag.String("host", "", "address users reach installed apps at (default: derived from -listen)")
	storage := flag.String("storage", "", "shared storage directory exposed to apps (default: <data>/storage)")
	tz := flag.String("tz", "", "timezone passed to apps (default: system timezone)")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)
	if err := run(*listen, web.Options{
		DataDir: *dataDir, CatalogRepo: *catalogRepo, AppsHost: *host, StorageDir: *storage, Timezone: *tz, Logger: logger,
	}, logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(listen string, opts web.Options, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if opts.AppsHost == "" {
		opts.AppsHost = defaultHost(listen)
	}
	docker, err := web.New(ctx, opts)
	if err != nil {
		return err
	}
	defer docker.Close()

	password := os.Getenv("DOCKER_GO_PASSWORD")
	if password == "" && !isLoopback(listen) {
		logger.Warn("serving without authentication on a non-loopback address; set DOCKER_GO_PASSWORD")
	}
	srv := &http.Server{
		Addr:              listen,
		Handler:           accessLog(logger, docker.Standalone(password)),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	logger.Info("panel listening", "url", "http://"+listen, "data", opts.DataDir, "apps_host", opts.AppsHost)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdown)
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return host == "localhost" || ip != nil && ip.IsLoopback()
}

// defaultHost picks the address apps are advertised at: the listen host, or
// the primary outbound IP when listening on all interfaces.
func defaultHost(listen string) string {
	h, _, err := net.SplitHostPort(listen)
	if err != nil {
		return "localhost"
	}
	switch h {
	case "127.0.0.1", "::1", "localhost":
		return "localhost"
	case "", "0.0.0.0", "::":
		// A UDP "connection" sends nothing; it just selects the route.
		if conn, err := net.Dial("udp", "192.0.2.1:9"); err == nil {
			defer conn.Close()
			return conn.LocalAddr().(*net.UDPAddr).IP.String()
		}
		return "localhost"
	}
	return h
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// Flush and Hijack are forwarded for SSE and WebSocket handlers that use
// type assertions rather than http.ResponseController.
func (r *statusRecorder) Flush() { http.NewResponseController(r.ResponseWriter).Flush() }

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	r.status = http.StatusSwitchingProtocols
	return http.NewResponseController(r.ResponseWriter).Hijack()
}

func accessLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			logger.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status, "duration", time.Since(start).Round(time.Millisecond).String())
		}
	})
}
