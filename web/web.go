// Package web serves the panel's browser UI: Preact + HTM as native ES
// modules, embedded in the binary, with no build step.
package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed static
var embedded embed.FS

// csp only allows the panel's own scripts, styles, images and connections
// (including its WebSocket terminal).
const csp = "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; " +
	"connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'"

// Handler serves the UI. Paths are relative to the mount point, so the UI
// works under any prefix; the API is expected at "api/" beside it.
func Handler() http.Handler {
	files, err := fs.Sub(embedded, "static")
	if err != nil {
		panic(err) // the embed directive guarantees the directory exists
	}
	etags := map[string]string{}
	fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			data, _ := fs.ReadFile(files, p)
			sum := sha256.Sum256(data)
			etags[p] = `"` + hex.EncodeToString(sum[:8]) + `"`
		}
		return err
	})
	server := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		etag, ok := etags[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		h := w.Header()
		h.Set("ETag", etag)
		// Embedded files change only with the binary: revalidate cheaply.
		h.Set("Cache-Control", "no-cache")
		h.Set("X-Content-Type-Options", "nosniff")
		if name == "index.html" {
			h.Set("Content-Security-Policy", csp)
			h.Set("Referrer-Policy", "same-origin")
			http.ServeFileFS(w, r, files, name) // FileServer would redirect index.html to ./
			return
		}
		server.ServeHTTP(w, r)
	})
}
