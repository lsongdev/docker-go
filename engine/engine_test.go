package engine_test

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lsongdev/docker-go/engine"
	"github.com/lsongdev/docker-go/engine/enginetest"
)

func TestVersionNegotiation(t *testing.T) {
	for _, tc := range []struct{ server, want, err string }{
		{"1.45", "1.45", ""},
		{"9.99", engine.MaxAPIVersion, ""},
		{"1.40", "", "older than the minimum"},
	} {
		d, c := enginetest.New(t)
		d.APIVersion = tc.server
		got, err := c.APIVersion(context.Background())
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("server %s: err = %v, want %q", tc.server, err, tc.err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("server %s: got %q, %v; want %q", tc.server, got, err, tc.want)
		}
	}
}

func TestRequestsUseNegotiatedVersion(t *testing.T) {
	d, c := enginetest.New(t)
	d.APIVersion = "1.45"
	var path string
	d.Mux.HandleFunc("GET /info", func(w http.ResponseWriter, r *http.Request) {
		path = r.RequestURI
		w.Write([]byte(`{"Name":"host"}`))
	})
	info, err := c.Info(context.Background())
	if err != nil || info.Name != "host" {
		t.Fatalf("Info = %+v, %v", info, err)
	}
	// RequestURI keeps the original path the fake strips before routing.
	if path != "/v1.45/info" {
		t.Fatalf("routed path = %q", path)
	}
}

func TestErrorDecoding(t *testing.T) {
	d, c := enginetest.New(t)
	d.Error("GET /containers/missing/json", http.StatusNotFound, "No such container: missing")
	d.Error("DELETE /images/busy", http.StatusConflict, "image is in use")
	_, err := c.InspectContainer(context.Background(), "missing")
	var e *engine.Error
	if !errors.As(err, &e) || e.Message != "No such container: missing" || !engine.IsNotFound(err) {
		t.Fatalf("err = %#v", err)
	}
	_, err = c.RemoveImage(context.Background(), "busy", engine.RemoveImageOptions{})
	if !engine.IsConflict(err) {
		t.Fatalf("err = %v, want conflict", err)
	}
}

func TestListContainersFilters(t *testing.T) {
	d, c := enginetest.New(t)
	var query string
	d.Mux.HandleFunc("GET /containers/json", func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.Write([]byte(`[{"Id":"abc","Names":["/web"],"State":"running"}]`))
	})
	list, err := c.ListContainers(context.Background(), engine.ListContainersOptions{All: true, Filters: engine.Filters{"label": {"a=b"}}})
	if err != nil || len(list) != 1 || list[0].Name() != "web" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if !strings.Contains(query, "all=1") || !strings.Contains(query, "filters=%7B%22label%22%3A%5B%22a%3Db%22%5D%7D") {
		t.Fatalf("query = %s", query)
	}
}

// frame encodes one multiplexed stream frame.
func frame(stream byte, data string) []byte {
	header := make([]byte, 8)
	header[0] = stream
	binary.BigEndian.PutUint32(header[4:], uint32(len(data)))
	return append(header, data...)
}

func TestLogsMultiplexed(t *testing.T) {
	d, c := enginetest.New(t)
	d.Mux.HandleFunc("GET /containers/x/logs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.docker.multiplexed-stream")
		var b bytes.Buffer
		b.Write(frame(1, "hello wo"))
		b.Write(frame(2, "oops\n"))
		b.Write(frame(1, "rld\nsecond\r\n"))
		b.Write(frame(1, "partial"))
		w.Write(b.Bytes())
	})
	var got []engine.LogLine
	err := c.Logs(context.Background(), "x", engine.LogsOptions{Tail: "10"}, func(l engine.LogLine) error {
		got = append(got, l)
		return nil
	})
	want := []engine.LogLine{{engine.Stderr, "oops"}, {engine.Stdout, "hello world"}, {engine.Stdout, "second"}, {engine.Stdout, "partial"}}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("got %v, %v; want %v", got, err, want)
	}
}

func TestLogsRawTTY(t *testing.T) {
	d, c := enginetest.New(t)
	d.Mux.HandleFunc("GET /containers/x/logs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.docker.raw-stream")
		w.Write([]byte("a\nb\n"))
	})
	var got []string
	c.Logs(context.Background(), "x", engine.LogsOptions{}, func(l engine.LogLine) error {
		got = append(got, string(l.Stream)+":"+l.Text)
		return nil
	})
	if !slices.Equal(got, []string{"stdout:a", "stdout:b"}) {
		t.Fatalf("got %v", got)
	}
}

func TestPullReportsStreamErrors(t *testing.T) {
	d, c := enginetest.New(t)
	var query string
	d.Mux.HandleFunc("POST /images/create", func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.Write([]byte(`{"status":"Pulling from library/nginx","id":"latest"}` + "\n"))
		w.Write([]byte(`{"errorDetail":{"message":"manifest unknown"},"error":"manifest unknown"}` + "\n"))
	})
	var messages []string
	err := c.PullImage(context.Background(), "nginx", engine.PullOptions{}, func(m engine.Message) error {
		messages = append(messages, m.Text())
		return nil
	})
	if err == nil || err.Error() != "manifest unknown" {
		t.Fatalf("err = %v", err)
	}
	if query != "fromImage=nginx&tag=latest" || len(messages) != 1 || messages[0] != "latest: Pulling from library/nginx" {
		t.Fatalf("query %q messages %q", query, messages)
	}
}

func TestParseReference(t *testing.T) {
	for ref, want := range map[string][2]string{
		"nginx":                     {"nginx", "latest"},
		"nginx:1.27":                {"nginx", "1.27"},
		"registry:5000/team/app":    {"registry:5000/team/app", "latest"},
		"registry:5000/team/app:v2": {"registry:5000/team/app", "v2"},
		"ghcr.io/o/app@sha256:abc":  {"ghcr.io/o/app", "sha256:abc"},
		"  library/alpine:3.20  ":   {"library/alpine", "3.20"},
	} {
		repo, tag := engine.ParseReference(ref)
		if repo != want[0] || tag != want[1] {
			t.Errorf("ParseReference(%q) = %q, %q; want %q", ref, repo, tag, want)
		}
	}
}

func TestStatsSummary(t *testing.T) {
	var s engine.StatsJSON
	json.Unmarshal([]byte(`{
		"cpu_stats": {"cpu_usage": {"total_usage": 400}, "system_cpu_usage": 2000, "online_cpus": 4},
		"precpu_stats": {"cpu_usage": {"total_usage": 200}, "system_cpu_usage": 1000},
		"memory_stats": {"usage": 1000, "limit": 4000, "stats": {"inactive_file": 200}},
		"networks": {"eth0": {"rx_bytes": 10, "tx_bytes": 5}, "eth1": {"rx_bytes": 1, "tx_bytes": 1}},
		"blkio_stats": {"io_service_bytes_recursive": [{"op": "Read", "value": 7}, {"op": "write", "value": 3}]}
	}`), &s)
	got := s.Summary()
	// (400-200)/(2000-1000) * 4 cpus * 100
	if got.CPUPercent != 80 || got.MemoryUsage != 800 || got.MemoryPercent != 20 {
		t.Fatalf("cpu/mem = %+v", got)
	}
	if got.NetworkRx != 11 || got.NetworkTx != 6 || got.BlockRead != 7 || got.BlockWrite != 3 {
		t.Fatalf("io = %+v", got)
	}
}

func TestTarDirectoryHonoursDockerignore(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"Dockerfile":              "FROM scratch",
		".dockerignore":           "node_modules\n*.log\n!keep.log\n**/secret.txt\n",
		"app/main.go":             "package main",
		"app/secret.txt":          "x",
		"node_modules/x/index.js": "x",
		"debug.log":               "x",
		"keep.log":                "x",
	} {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(content), 0o644)
	}
	rc := engine.TarDirectory(dir)
	defer rc.Close()
	tr := tar.NewReader(rc)
	var names []string
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			names = append(names, h.Name)
		}
	}
	slices.Sort(names)
	want := []string{".dockerignore", "Dockerfile", "app/main.go", "keep.log"}
	if !slices.Equal(names, want) {
		t.Fatalf("got %v, want %v", names, want)
	}
}
