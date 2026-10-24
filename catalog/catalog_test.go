package catalog

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// writeCatalog creates a catalog directory from path → content pairs.
func writeCatalog(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const compose = "services:\n  web:\n    image: nginx\n"

func testCatalog(t *testing.T) *Catalog {
	dir := writeCatalog(t, map[string]string{
		"web/manifest.json":            `{"id":"ignored","name":"Web Server","version":"1.0","available":true,"categories":["network"],"short_desc":"Serves pages","icon":"logo.jpg","supported_architectures":["amd64","arm64"]}`,
		"web/docker-compose.yml":       compose,
		"web/docker-compose.arm64.yml": "services:\n  web:\n    image: nginx:arm\n",
		"web/logo.jpg":                 "img",
		"web/data/seed.conf":           "seed",
		"web/README.md":                "# Web",
		"db/manifest.json":             `{"name":"database","version":"2","available":true,"categories":["data"],"supported_architectures":["amd64"]}`,
		"db/docker-compose.yml":        compose,
		"old/manifest.json":            `{"name":"Old","available":true,"deprecated":true}`,
		"old/docker-compose.yml":       compose,
		"hidden/manifest.json":         `{"name":"Hidden","available":false}`,
		"hidden/docker-compose.yml":    compose,
		"broken/manifest.json":         `{not json`,
		"broken/docker-compose.yml":    compose,
		"nocompose/manifest.json":      `{"name":"x","available":true}`,
		".git/manifest.json":           `{}`,
	})
	c := New(dir, "")
	if err := c.Load(); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestLoadAndQuery(t *testing.T) {
	c := testCatalog(t)
	if c.Len() != 4 { // web, db, old, hidden
		t.Fatalf("Len = %d", c.Len())
	}
	var ids []string
	for _, a := range c.List(Query{}) {
		ids = append(ids, a.ID)
	}
	if !slices.Equal(ids, []string{"db", "web"}) { // sorted by name, hidden/deprecated excluded
		t.Fatalf("List = %v", ids)
	}
	if got := c.List(Query{Search: "pages"}); len(got) != 1 || got[0].ID != "web" {
		t.Fatalf("search = %v", got)
	}
	if got := c.List(Query{Category: "data"}); len(got) != 1 || got[0].ID != "db" {
		t.Fatalf("category = %v", got)
	}
	if app, err := c.Get("web"); err != nil || app.ID != "web" { // directory name wins over manifest id
		t.Fatalf("Get = %+v, %v", app, err)
	}
	if _, err := c.Get("hidden"); err != nil {
		t.Fatalf("unavailable apps stay resolvable for installed copies: %v", err)
	}
	if _, err := c.Get("nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing: %v", err)
	}
	cats := c.Categories()
	if len(cats) != 2 || cats[0].Name != "data" || cats[1].Count != 1 {
		t.Fatalf("Categories = %+v", cats)
	}
}

func TestTemplateFiles(t *testing.T) {
	c := testCatalog(t)
	if data, _ := c.ComposeFile("web", "arm64"); !strings.Contains(string(data), "nginx:arm") {
		t.Fatalf("arm64 variant not preferred: %s", data)
	}
	if data, _ := c.ComposeFile("web", "amd64"); string(data) != compose {
		t.Fatalf("default compose: %s", data)
	}
	if seeds, _ := c.SeedEntries("web"); !slices.Equal(seeds, []string{"data"}) {
		t.Fatalf("SeedEntries = %v", seeds)
	}
	if p, err := c.IconPath("web"); err != nil || filepath.Base(p) != "logo.jpg" {
		t.Fatalf("IconPath = %q, %v", p, err)
	}
	if _, err := c.IconPath("db"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("db icon: %v", err)
	}
	if readme, _ := c.Readme("web"); readme != "# Web" {
		t.Fatalf("Readme = %q", readme)
	}
	db, _ := c.Get("db")
	if db.SupportsArch("arm64") || !db.SupportsArch("amd64") {
		t.Fatal("SupportsArch")
	}
}

func TestResolveValues(t *testing.T) {
	app := &App{FormFields: []FormField{
		{Type: FieldText, EnvVariable: "USER", Required: true, Min: new(3)},
		{Type: FieldRandom, EnvVariable: "SECRET", Min: new(16)},
		{Type: FieldRandom, EnvVariable: "B64", Min: new(12), Encoding: "base64"},
		{Type: FieldNumber, EnvVariable: "WORKERS", Min: new(1), Max: new(8), Default: float64(2)},
		{Type: FieldBoolean, EnvVariable: "SIGNUP", Default: true},
		{Type: FieldText, EnvVariable: "MODE", Options: []Option{{"Fast", "fast"}, {"Safe", "safe"}}},
		{Type: FieldEmail, EnvVariable: "EMAIL"},
		{Type: FieldText, EnvVariable: "SLUG", Regex: "^[a-z]+$", PatternError: "lowercase only"},
	}}

	values, err := app.ResolveValues(map[string]string{"USER": "admin", "SIGNUP": "1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if values["USER"] != "admin" || values["WORKERS"] != "2" || values["SIGNUP"] != "true" || values["MODE"] != "" {
		t.Fatalf("values = %v", values)
	}
	if len(values["SECRET"]) != 16 || strings.Trim(values["SECRET"], secretAlphabet) != "" {
		t.Fatalf("SECRET = %q", values["SECRET"])
	}
	if len(values["B64"]) != 16 { // base64 of 12 bytes
		t.Fatalf("B64 = %q", values["B64"])
	}

	// Secrets survive reconfiguration; omitted fields keep previous values.
	again, err := app.ResolveValues(map[string]string{"WORKERS": "4"}, values)
	if err != nil || again["SECRET"] != values["SECRET"] || again["USER"] != "admin" || again["WORKERS"] != "4" {
		t.Fatalf("again = %v, %v", again, err)
	}

	_, err = app.ResolveValues(map[string]string{"USER": "ab", "WORKERS": "9", "MODE": "turbo", "EMAIL": "nope", "SLUG": "Bad"}, nil)
	var fields FieldErrors
	if !errors.As(err, &fields) {
		t.Fatalf("err = %v", err)
	}
	for key, want := range map[string]string{"USER": "at least 3", "WORKERS": "at most 8", "MODE": "options", "EMAIL": "email", "SLUG": "lowercase only"} {
		if !strings.Contains(fields[key], want) {
			t.Errorf("%s: %q, want %q", key, fields[key], want)
		}
	}
	if _, err := app.ResolveValues(map[string]string{}, nil); err == nil || !strings.Contains(err.Error(), "USER: required") {
		t.Fatalf("required: %v", err)
	}
}

func TestOptionValuesAcceptNumbers(t *testing.T) {
	var f FormField
	if err := json.Unmarshal([]byte(`{"type":"text","env_variable":"N","options":[{"label":"One","value":1},{"label":"Yes","value":true}]}`), &f); err != nil {
		t.Fatal(err)
	}
	if f.Options[0].Value != "1" || f.Options[1].Value != "true" {
		t.Fatalf("options = %+v", f.Options)
	}
}
