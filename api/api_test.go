package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lsongdev/docker-go/apps"
	"github.com/lsongdev/docker-go/catalog"
	"github.com/lsongdev/docker-go/engine"
	"github.com/lsongdev/docker-go/engine/enginetest"
	"github.com/lsongdev/docker-go/jobs"
)

type envelope struct {
	Data  json.RawMessage `json:"data"`
	Error *apiError       `json:"error"`
}

func newTestServer(t *testing.T) (*httptest.Server, *enginetest.Daemon) {
	t.Helper()
	catDir := t.TempDir()
	os.MkdirAll(filepath.Join(catDir, "notes"), 0o755)
	os.WriteFile(filepath.Join(catDir, "notes/manifest.json"), []byte(`{"name":"Notes","available":true,"port":9000,"form_fields":[{"type":"email","label":"Email","env_variable":"EMAIL","required":true}]}`), 0o644)
	os.WriteFile(filepath.Join(catDir, "notes/docker-compose.yml"), []byte("services: {}\n"), 0o644)
	cat := catalog.New(catDir, "")
	if err := cat.Load(); err != nil {
		t.Fatal(err)
	}
	d, client := enginetest.New(t)
	d.JSON("GET /version", engine.Version{Arch: "amd64"})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := &Server{
		Engine:  client,
		Catalog: cat,
		Apps:    newApps(t, cat, client),
		Jobs:    jobs.NewManager(ctx, 10),
	}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv, d
}

func call(t *testing.T, method, url, body string) (int, envelope) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var e envelope
	json.NewDecoder(resp.Body).Decode(&e)
	return resp.StatusCode, e
}

func TestListContainersDTO(t *testing.T) {
	srv, d := newTestServer(t)
	d.JSON("GET /containers/json", []engine.Container{{
		ID: "abc", Names: []string{"/web"}, State: "running",
		Labels: map[string]string{"com.docker.compose.project": "site"},
		// IPv4 and IPv6 bindings of the same port collapse into one entry.
		Ports: []engine.Port{{IP: "0.0.0.0", PrivatePort: 80, PublicPort: 8080, Type: "tcp"}, {IP: "::", PrivatePort: 80, PublicPort: 8080, Type: "tcp"}},
	}})
	status, e := call(t, "GET", srv.URL+"/containers", "")
	var list []containerSummary
	json.Unmarshal(e.Data, &list)
	if status != 200 || len(list) != 1 || list[0].Name != "web" || list[0].Project != "site" || len(list[0].Ports) != 1 {
		t.Fatalf("status %d list %+v", status, list)
	}
}

func TestErrorMapping(t *testing.T) {
	srv, d := newTestServer(t)
	d.Error("GET /containers/nope/json", 404, "No such container: nope")
	d.Error("DELETE /networks/busy", 403, "network has active endpoints")
	for _, tc := range []struct {
		method, path, body string
		status             int
		code               string
	}{
		{"GET", "/containers/nope", "", 404, "not_found"},
		{"DELETE", "/networks/busy", "", 400, "docker_rejected"},
		{"GET", "/does/not/exist", "", 404, "not_found"},
		{"POST", "/volumes", `{"name":"x","bogus":1}`, 400, "invalid_request"},
		{"POST", "/containers", `{"image":""}`, 400, "invalid_request"},
		{"GET", "/catalog/missing", "", 404, "not_found"},
		{"GET", "/apps/missing", "", 404, "not_found"},
		{"GET", "/jobs/zzz", "", 404, "not_found"},
	} {
		status, e := call(t, tc.method, srv.URL+tc.path, tc.body)
		if status != tc.status || e.Error == nil || e.Error.Code != tc.code {
			t.Errorf("%s %s: %d %+v, want %d %s", tc.method, tc.path, status, e.Error, tc.status, tc.code)
		}
	}
	// A body without a JSON content type is rejected before decoding.
	req, _ := http.NewRequest("POST", srv.URL+"/volumes", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "text/plain")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Errorf("text/plain body: %d", resp.StatusCode)
	}
}

func TestCrossOriginWritesRejected(t *testing.T) {
	srv, _ := newTestServer(t)
	req, _ := http.NewRequest("POST", srv.URL+"/containers/x/start", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestInstallValidationReturnsFieldErrors(t *testing.T) {
	srv, d := newTestServer(t)
	d.JSON("GET /containers/json", []engine.Container{})
	status, e := call(t, "POST", srv.URL+"/apps", `{"id":"notes","values":{"EMAIL":"not-an-email"}}`)
	if status != 422 || e.Error == nil || e.Error.Fields["EMAIL"] == "" {
		t.Fatalf("status %d error %+v", status, e.Error)
	}
}

func TestPullRunsAsJob(t *testing.T) {
	srv, d := newTestServer(t)
	d.Mux.HandleFunc("POST /images/create", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"Pulling from library/alpine","id":"3.20"}` + "\n" + `{"status":"Download complete","id":"abc"}` + "\n"))
	})
	status, e := call(t, "POST", srv.URL+"/images/pull", `{"image":"alpine:3.20"}`)
	var job jobs.Snapshot
	json.Unmarshal(e.Data, &job)
	if status != 202 || job.ID == "" || job.Target != "image:alpine:3.20" {
		t.Fatalf("status %d job %+v", status, job)
	}
	resp, err := http.Get(srv.URL + "/jobs/" + job.ID + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content type %q", ct)
	}
	var events, lines []string
	var done jobs.Snapshot
	scanner := bufio.NewScanner(resp.Body)
	event := ""
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
			events = append(events, event)
		case strings.HasPrefix(line, "data: ") && event == "done":
			json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &done)
		case strings.HasPrefix(line, "data: ") && event == "snapshot":
			var snap struct{ Lines []string }
			json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &snap)
			lines = append(lines, snap.Lines...)
		case strings.HasPrefix(line, "data: ") && event == "line":
			var l string
			json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &l)
			lines = append(lines, l)
		}
	}
	if events[0] != "snapshot" || events[len(events)-1] != "done" || done.Status != jobs.Succeeded {
		t.Fatalf("events %v done %+v", events, done)
	}
	if !slices.Contains(lines, "Pulled alpine:3.20") {
		t.Fatalf("lines %v", lines)
	}
}

func TestParsePortSpec(t *testing.T) {
	for spec, want := range map[string][3]string{ // port, hostIP, hostPort ("-" = expose only)
		"80":                  {"80/tcp", "-", "-"},
		"53/udp":              {"53/udp", "-", "-"},
		"8080:80":             {"80/tcp", "", "8080"},
		":80":                 {"80/tcp", "", ""},
		"127.0.0.1:5432:5432": {"5432/tcp", "127.0.0.1", "5432"},
		"[::1]:8443:443/tcp":  {"443/tcp", "::1", "8443"},
	} {
		port, binding, err := parsePortSpec(spec)
		if err != nil {
			t.Errorf("%s: %v", spec, err)
			continue
		}
		got := [3]string{port, "-", "-"}
		if binding != nil {
			got[1], got[2] = binding.HostIP, binding.HostPort
		}
		if got != want {
			t.Errorf("%s: got %v, want %v", spec, got, want)
		}
	}
	for _, bad := range []string{"", "abc", "70000:80", "80/icmp", "1:2:3:4", "bad-ip:80:80", "[::1]80"} {
		if _, _, err := parsePortSpec(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}

func TestSplitWords(t *testing.T) {
	for in, want := range map[string][]string{
		``:                          nil,
		`nginx -g 'daemon off;'`:    {"nginx", "-g", "daemon off;"},
		`sh -c "echo \"hi\" there"`: {"sh", "-c", `echo "hi" there`},
		`a\ b  c`:                   {"a b", "c"},
		`''`:                        {""},
	} {
		got, err := splitWords(in)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("splitWords(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := splitWords(`echo "open`); err == nil {
		t.Error("unterminated quote accepted")
	}
}

func TestCreateContainerSpec(t *testing.T) {
	req := createContainerRequest{
		Image: "nginx", Command: "nginx -g 'daemon off;'", Env: []string{"A=1", ""}, Ports: []string{"8080:80"},
		Volumes: []string{"data:/data:ro"}, Restart: "unless-stopped", Labels: []string{"k=v"},
	}
	spec, err := req.spec()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(spec.Cmd, []string{"nginx", "-g", "daemon off;"}) || !slices.Equal(spec.Env, []string{"A=1"}) || spec.Labels["k"] != "v" {
		t.Fatalf("config = %+v", spec.ContainerConfig)
	}
	h := spec.HostConfig
	if h.RestartPolicy.Name != "unless-stopped" || h.PortBindings["80/tcp"][0].HostPort != "8080" || h.Binds[0] != "data:/data:ro" {
		t.Fatalf("host = %+v", h)
	}
	for _, bad := range []createContainerRequest{
		{Image: "x", Env: []string{"NOEQUALS"}},
		{Image: "x", Volumes: []string{"relative"}},
		{Image: "x", Restart: "sometimes"},
		{Image: "x", Name: "bad name"},
	} {
		if _, err := bad.spec(); err == nil {
			t.Errorf("%+v: expected error", bad)
		}
	}
}

// List fields are always JSON arrays, never null, so clients can iterate
// without guards.
func TestImageDetailArraysNeverNull(t *testing.T) {
	srv, d := newTestServer(t)
	d.JSON("GET /images/alpine/json", engine.ImageInspect{ID: "sha256:abc", Config: &engine.ContainerConfig{}})
	d.JSON("GET /images/sha256:abc/history", []engine.HistoryEntry{})
	d.JSON("GET /containers/json", []engine.Container{})
	_, e := call(t, "GET", srv.URL+"/images/alpine", "")
	var raw map[string]any
	json.Unmarshal(e.Data, &raw)
	for _, key := range []string{"tags", "digests", "env", "exposed_ports", "volumes", "history", "containers"} {
		if _, ok := raw[key].([]any); !ok {
			t.Errorf("%s = %#v, want array", key, raw[key])
		}
	}
}

func TestImageReferencesWithSlashes(t *testing.T) {
	srv, d := newTestServer(t)
	d.JSON("DELETE /images/ghcr.io/org/app:v1", []engine.ImageDeleteItem{{Untagged: "ghcr.io/org/app:v1"}})
	status, e := call(t, "DELETE", srv.URL+"/images/ghcr.io/org/app:v1", "")
	if status != 200 || e.Error != nil {
		t.Fatalf("status %d error %+v", status, e.Error)
	}
}

func TestDaemonDownIsServiceUnavailable(t *testing.T) {
	// A short path: a temporary directory can exceed the socket path limit.
	client, _ := engine.New(engine.Config{Host: "unix:///nonexistent/docker.sock"})
	srv := httptest.NewServer((&Server{Engine: client}).Handler())
	defer srv.Close()
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	status, e := call(t, "GET", srv.URL+"/containers", "")
	if status != 503 || e.Error == nil || e.Error.Code != "docker_not_installed" {
		t.Fatalf("without docker: status %d error %+v", status, e.Error)
	}
	// Installed but not running.
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	status, e = call(t, "GET", srv.URL+"/containers", "")
	if status != 503 || e.Error == nil || e.Error.Code != "docker_unavailable" {
		t.Fatalf("with docker: status %d error %+v", status, e.Error)
	}
}

func newApps(t *testing.T, cat *catalog.Catalog, client *engine.Client) *apps.Manager {
	root := t.TempDir()
	return apps.NewManager(root, apps.NewFileStore(root), cat, nil, client, apps.Settings{})
}

func TestAppDetailBeforeAndAfterInstallation(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "notes"), 0755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dir, "notes/manifest.json")
	os.WriteFile(manifest, []byte(`{"name":"Notes","available":true,"port":9000,"supported_architectures":["amd64"]}`), 0644)
	os.WriteFile(filepath.Join(dir, "notes/docker-compose.yml"), []byte("services: {}\n"), 0644)
	os.WriteFile(filepath.Join(dir, "notes/README.md"), []byte("App introduction"), 0644)
	cat := catalog.New(dir, "")
	if err := cat.Load(); err != nil {
		t.Fatal(err)
	}
	d, client := enginetest.New(t)
	d.JSON("GET /version", engine.Version{Arch: "amd64"})
	var daemonFailure atomic.Bool
	d.Mux.HandleFunc("GET /containers/json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if daemonFailure.Load() {
			w.WriteHeader(500)
			w.Write([]byte(`{"message":"daemon failure"}`))
			return
		}
		w.Write([]byte("[]"))
	})
	root := t.TempDir()
	store := apps.NewFileStore(root)
	server := &Server{Engine: client, Catalog: cat, Apps: apps.NewManager(root, store, cat, nil, client, apps.Settings{})}
	srv := httptest.NewServer(server.Handler())
	defer srv.Close()
	type detail struct {
		App       *apps.Installed `json:"app"`
		Catalog   *catalog.App    `json:"catalog"`
		Readme    string          `json:"readme"`
		Installed bool            `json:"installed"`
		Supported bool            `json:"supported"`
	}
	get := func() detail {
		t.Helper()
		status, e := call(t, "GET", srv.URL+"/apps/notes", "")
		if status != 200 {
			t.Fatalf("detail status=%d error=%+v", status, e.Error)
		}
		var result detail
		if err := json.Unmarshal(e.Data, &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := get()
	if before.Installed || before.App != nil || before.Catalog == nil || !before.Supported || before.Readme != "App introduction" {
		t.Fatalf("before install: %+v", before)
	}
	if err := store.PutApp(context.Background(), apps.Record{ID: "notes", Version: "1", Port: 9000}); err != nil {
		t.Fatal(err)
	}
	after := get()
	if !after.Installed || after.App == nil || after.Catalog == nil || after.App.Port != 9000 || after.Readme != before.Readme {
		t.Fatalf("after install: %+v", after)
	}
	// Removing a catalog entry must not hide an installed app's controls.
	os.MkdirAll(filepath.Join(dir, "other"), 0755)
	os.WriteFile(filepath.Join(dir, "other/manifest.json"), []byte(`{"name":"Other","available":true}`), 0644)
	os.WriteFile(filepath.Join(dir, "other/docker-compose.yml"), []byte("services: {}\n"), 0644)
	os.Remove(manifest)
	if err := cat.Load(); err != nil {
		t.Fatal(err)
	}
	removed := get()
	if !removed.Installed || removed.App == nil || removed.Catalog != nil {
		t.Fatalf("removed template: %+v", removed)
	}
	os.WriteFile(manifest, []byte(`{"name":"Notes","available":true,"supported_architectures":["arm64"]}`), 0644)
	if err := cat.Load(); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteApp(context.Background(), "notes"); err != nil {
		t.Fatal(err)
	}
	uninstalled := get()
	if uninstalled.Installed || uninstalled.App != nil || uninstalled.Catalog == nil || uninstalled.Supported {
		t.Fatalf("after uninstall / unsupported arch: %+v", uninstalled)
	}
	// Engine errors are not misrepresented as an uninstalled app.
	store.PutApp(context.Background(), apps.Record{ID: "notes", Version: "1"})
	daemonFailure.Store(true)
	status, _ := call(t, "GET", srv.URL+"/apps/notes", "")
	if status == 200 {
		t.Fatal("daemon failure was hidden")
	}
}
