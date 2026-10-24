package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler(t *testing.T) {
	h := Handler()
	get := func(path string, header http.Header) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		for k, v := range header {
			req.Header[k] = v
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}

	index := get("/", nil)
	if index.Code != 200 || !strings.Contains(index.Body.String(), `src="app.js"`) {
		t.Fatalf("index: %d", index.Code)
	}
	if csp := index.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
		t.Fatalf("csp = %q", csp)
	}
	etag := index.Header().Get("ETag")
	if etag == "" {
		t.Fatal("missing ETag")
	}
	if w := get("/", http.Header{"If-None-Match": {etag}}); w.Code != http.StatusNotModified {
		t.Fatalf("revalidation: %d", w.Code)
	}
	if w := get("/lib/html.js", nil); w.Code != 200 || !strings.Contains(w.Header().Get("Content-Type"), "javascript") {
		t.Fatalf("module: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	for _, path := range []string{"/missing.js", "/../web.go", "/vendor/"} {
		if w := get(path, nil); w.Code != 404 {
			t.Errorf("%s: %d", path, w.Code)
		}
	}
	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d", w.Code)
	}
}
