package apps

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/lsongdev/docker-go/catalog"
	"github.com/lsongdev/docker-go/engine"
	"github.com/lsongdev/docker-go/engine/enginetest"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// newManager builds a manager over a two-app catalog and a fake daemon
// with one container publishing port 8080.
func newManager(t *testing.T) (*Manager, *enginetest.Daemon) {
	t.Helper()
	catDir := t.TempDir()
	writeFiles(t, catDir, map[string]string{
		"notes/manifest.json":      `{"name":"Notes","version":"2.0","available":true,"port":8080,"supported_architectures":["arm64","amd64"],"form_fields":[{"type":"text","label":"Admin","env_variable":"ADMIN","required":true},{"type":"random","label":"Key","env_variable":"KEY","min":8}]}`,
		"notes/docker-compose.yml": "services:\n  notes:\n    image: notes\n",
		"notes/data/seed.conf":     "default",
		"x86/manifest.json":        `{"name":"X86 only","version":"1","available":true,"supported_architectures":["amd64"]}`,
		"x86/docker-compose.yml":   "services: {}\n",
	})
	cat := catalog.New(catDir, "")
	if err := cat.Load(); err != nil {
		t.Fatal(err)
	}
	d, client := enginetest.New(t)
	d.JSON("GET /version", engine.Version{Arch: "arm64"})
	d.JSON("GET /containers/json", []engine.Container{{ID: "c1", Names: []string{"/other"}, Ports: []engine.Port{{PrivatePort: 80, PublicPort: 8080, Type: "tcp"}}}})
	root := t.TempDir()
	return NewManager(root, NewFileStore(root), cat, nil, client, Settings{Host: "10.0.0.5", StorageDir: "/srv/storage"}), d
}

func TestPlanInstallValidation(t *testing.T) {
	m, _ := newManager(t)
	ctx := context.Background()

	_, err := m.PlanInstall(ctx, "notes", Request{Values: map[string]string{"ADMIN": "me"}})
	var fields catalog.FieldErrors
	if !errors.As(err, &fields) || !strings.Contains(fields["port"], "used by other") {
		t.Fatalf("default port 8080 is taken: %v", err)
	}
	_, err = m.PlanInstall(ctx, "notes", Request{Port: 9000})
	if !errors.As(err, &fields) || fields["ADMIN"] != "required" {
		t.Fatalf("required field: %v", err)
	}
	if _, err := m.PlanInstall(ctx, "x86", Request{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unsupported arch: %v", err)
	}
	plan, err := m.PlanInstall(ctx, "notes", Request{Port: 9000, Values: map[string]string{"ADMIN": "me"}})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Fresh || plan.Record.Port != 9000 || plan.Record.Version != "2.0" || len(plan.Record.Values["KEY"]) != 8 {
		t.Fatalf("plan = %+v", plan.Record)
	}

	// Once a record exists the app counts as installed.
	if err := os.MkdirAll(m.dir("notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.store.PutApp(ctx, plan.Record); err != nil {
		t.Fatal(err)
	}
	if _, err := m.PlanInstall(ctx, "notes", Request{Port: 9000}); !errors.Is(err, ErrInstalled) {
		t.Fatalf("second install: %v", err)
	}
	update, err := m.PlanUpdate(ctx, "notes", Request{})
	if err != nil || update.Fresh || update.Record.Values["KEY"] != plan.Record.Values["KEY"] || update.Record.Port != 9000 {
		t.Fatalf("update keeps secret and port: %+v, %v", update, err)
	}
	if _, err := m.PlanUpdate(ctx, "../etc", Request{}); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("path traversal id: %v", err)
	}
}

func TestEnvironmentPrecedence(t *testing.T) {
	m, _ := newManager(t)
	env := m.Environment(Record{ID: "notes", Port: 9000, Values: map[string]string{"ADMIN": "me", "APP_PORT": "1"}})
	for key, want := range map[string]string{
		"ADMIN":            "me",
		"APP_PORT":         "9000", // host values win over form values
		"APP_DOMAIN":       "10.0.0.5:9000",
		"APP_DATA_DIR":     m.dir("notes"),
		"ROOT_FOLDER_HOST": "/srv/storage",
		"TZ":               "UTC",
		"LOCAL_DOMAIN":     "local",
	} {
		if env[key] != want {
			t.Errorf("%s = %q, want %q", key, env[key], want)
		}
	}
}

func TestQuoteEnv(t *testing.T) {
	for in, want := range map[string]string{
		"plain":          `'plain'`,
		"with $VAR ${X}": `'with $VAR ${X}'`,
		`it's`:           `"it's"`,
		"a\nb":           `"a\nb"`,
		`q"'\`:           `"q\"'\\"`,
	} {
		if got := quoteEnv(in); got != want {
			t.Errorf("quoteEnv(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestSeedDoesNotOverwrite(t *testing.T) {
	m, _ := newManager(t)
	dir := m.dir("notes")
	writeFiles(t, dir, map[string]string{"data/seed.conf": "edited by user"})
	if err := m.seed("notes", dir, func(string) {}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "data/seed.conf"))
	if string(data) != "edited by user" {
		t.Fatalf("seed overwrote user data: %q", data)
	}
	other := t.TempDir()
	if err := m.seed("notes", other, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(other, "data/seed.conf")); string(data) != "default" {
		t.Fatalf("fresh seed = %q", data)
	}
}

func TestDescribeStatus(t *testing.T) {
	m, _ := newManager(t)
	record := Record{ID: "notes", Version: "1.0", Port: 9000}
	for name, tc := range map[string]struct {
		states []string
		want   Status
	}{
		"none":    {nil, StatusMissing},
		"all":     {[]string{"running", "running"}, StatusRunning},
		"partial": {[]string{"running", "exited"}, StatusPartial},
		"stopped": {[]string{"exited"}, StatusStopped},
	} {
		var containers []Container
		for _, s := range tc.states {
			containers = append(containers, Container{State: s})
		}
		got := m.describe(record, containers)
		if got.Status != tc.want {
			t.Errorf("%s: status %s, want %s", name, got.Status, tc.want)
		}
		if !got.UpdateAvailable || got.LatestVersion != "2.0" || got.URL != "http://10.0.0.5:9000" {
			t.Errorf("%s: %+v", name, got)
		}
	}
}

type fakeExposer struct{ calls []string }

func (f *fakeExposer) Expose(_ context.Context, e Exposure) error {
	f.calls = append(f.calls, "expose "+e.ID+" "+e.Domain+":"+strconv.Itoa(e.Port))
	return nil
}

func (f *fakeExposer) Unexpose(_ context.Context, id string) error {
	f.calls = append(f.calls, "unexpose "+id)
	return nil
}

func TestDomainNeedsExposer(t *testing.T) {
	m, _ := newManager(t)
	domain := "notes.example.com"
	_, err := m.PlanInstall(context.Background(), "notes", Request{Port: 9000, Domain: &domain, Values: map[string]string{"ADMIN": "me"}})
	var fields catalog.FieldErrors
	if !errors.As(err, &fields) || !strings.Contains(fields["domain"], "no reverse proxy") {
		t.Fatalf("err = %v", err)
	}
}

func TestDomainPublishing(t *testing.T) {
	m, _ := newManager(t)
	exposer := &fakeExposer{}
	m.settings.Exposer = exposer
	ctx := context.Background()

	bad := "not a domain"
	if _, err := m.PlanInstall(ctx, "notes", Request{Port: 9000, Domain: &bad, Values: map[string]string{"ADMIN": "me"}}); err == nil {
		t.Fatal("invalid domain accepted")
	}
	domain := "Notes.Example.com."
	plan, err := m.PlanInstall(ctx, "notes", Request{Port: 9000, Domain: &domain, Values: map[string]string{"ADMIN": "me"}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Record.Domain != "notes.example.com" {
		t.Fatalf("domain not normalised: %q", plan.Record.Domain)
	}
	env := m.Environment(plan.Record)
	if env["APP_DOMAIN"] != "notes.example.com" || env["APP_PROTOCOL"] != "https" || env["APP_EXPOSED"] != "true" {
		t.Fatalf("env = %v", env)
	}
	if got := m.describe(plan.Record, nil); got.URL != "https://notes.example.com" {
		t.Fatalf("URL = %q", got.URL)
	}
	if err := m.publish(ctx, plan, func(string) {}); err != nil {
		t.Fatal(err)
	}

	// Clearing the domain on update unpublishes the app.
	os.MkdirAll(m.dir("notes"), 0o755)
	m.store.PutApp(ctx, plan.Record)
	empty := ""
	update, err := m.PlanUpdate(ctx, "notes", Request{Domain: &empty})
	if err != nil {
		t.Fatal(err)
	}
	if err := m.publish(ctx, update, func(string) {}); err != nil {
		t.Fatal(err)
	}
	// Omitting the domain keeps it.
	keep, _ := m.PlanUpdate(ctx, "notes", Request{})
	if keep.Record.Domain != "notes.example.com" {
		t.Fatalf("domain dropped: %q", keep.Record.Domain)
	}
	want := []string{"expose notes notes.example.com:9000", "unexpose notes"}
	if strings.Join(exposer.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %v", exposer.calls)
	}
}

func TestFileStore(t *testing.T) {
	ctx := context.Background()
	s := NewFileStore(t.TempDir())
	if _, err := s.GetApp(ctx, "notes"); !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("missing app: %v", err)
	}
	if err := s.PutApp(ctx, Record{ID: "notes", Version: "1.0", Values: map[string]string{"K": "v"}}); err != nil {
		t.Fatal(err)
	}
	// A directory without a record (e.g. data kept after uninstall) is not an app.
	os.MkdirAll(filepath.Join(s.root, "kept"), 0o755)
	list, err := s.ListApps(ctx)
	if err != nil || len(list) != 1 || list[0].ID != "notes" || list[0].Values["K"] != "v" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if err := s.DeleteApp(ctx, "notes"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteApp(ctx, "notes"); err != nil {
		t.Fatalf("deleting a missing record: %v", err)
	}
}
