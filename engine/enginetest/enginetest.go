// Package enginetest provides a fake Docker daemon for tests.
package enginetest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lsongdev/docker-go/engine"
)

// Daemon is a fake Engine API. Routes are registered without the version
// prefix, e.g. "GET /containers/json"; /_ping is answered automatically.
type Daemon struct {
	Mux        *http.ServeMux
	APIVersion string
}

// New starts a fake daemon and returns it with a client connected to it.
func New(t testing.TB) (*Daemon, *engine.Client) {
	t.Helper()
	d := &Daemon{Mux: http.NewServeMux(), APIVersion: engine.MaxAPIVersion}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_ping" {
			w.Header().Set("API-Version", d.APIVersion)
			w.Write([]byte("OK"))
			return
		}
		// Strip the /v1.xx prefix so tests register version-independent routes.
		if strings.HasPrefix(r.URL.Path, "/v1.") {
			if i := strings.Index(r.URL.Path[1:], "/"); i >= 0 {
				r.URL.Path = r.URL.Path[i+1:]
			}
		}
		d.Mux.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	client, err := engine.New(engine.Config{Host: "tcp://" + strings.TrimPrefix(srv.URL, "http://")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	return d, client
}

// JSON registers a route answering with v as JSON.
func (d *Daemon) JSON(pattern string, v any) {
	d.Mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(v)
	})
}

// Error registers a route answering with a daemon error.
func (d *Daemon) Error(pattern string, status int, message string) {
	d.Mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]string{"message": message})
	})
}
