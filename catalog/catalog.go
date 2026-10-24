// Package catalog loads an app catalog from a git repository.
//
// The catalog is a directory of app templates, one per subdirectory:
//
//	<id>/manifest.json         metadata and install form
//	<id>/docker-compose.yml    compose template (variables are interpolated)
//	<id>/docker-compose.<arch>.yml  optional architecture-specific variant
//	<id>/logo.jpg, README.md   presentation
//	<id>/data, <id>/config ... optional seed files copied on install
//
// It is small enough to keep fully in memory, so it is reloaded from disk on
// every sync instead of being mirrored into a database.
package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("app not found")

// App is a catalog entry.
type App struct {
	ID            string      `json:"id"`
	Name          string      `json:"name"`
	Version       string      `json:"version"`
	ShortDesc     string      `json:"short_desc"`
	Description   string      `json:"description"`
	Author        string      `json:"author"`
	Source        string      `json:"source"`
	Website       string      `json:"website,omitempty"`
	Categories    []string    `json:"categories"`
	Port          int         `json:"port"`
	NoGUI         bool        `json:"no_gui"`
	HTTPS         bool        `json:"https"`
	URLSuffix     string      `json:"url_suffix,omitempty"`
	Exposable     bool        `json:"exposable"` // can be published at its own domain
	ForcePull     bool        `json:"force_pull"`
	Deprecated    bool        `json:"deprecated"`
	Architectures []string    `json:"supported_architectures"`
	FormFields    []FormField `json:"form_fields"`
	UID           *int        `json:"uid,omitempty"`
	GID           *int        `json:"gid,omitempty"`
	UpdatedAt     int64       `json:"updated_at"`

	available bool
	icon      string // file name inside the app directory
}

// SupportsArch reports whether the app runs on arch (amd64, arm64, ...). An
// app that declares no architectures is assumed to run everywhere.
func (a *App) SupportsArch(arch string) bool {
	return len(a.Architectures) == 0 || slices.Contains(a.Architectures, arch)
}

// manifest is the on-disk manifest.json shape.
type manifest struct {
	App
	Available bool   `json:"available"`
	Icon      string `json:"icon"`
}

// Catalog is an in-memory index of a catalog checkout. It is safe for
// concurrent use.
type Catalog struct {
	dir  string
	repo string // git URL; empty for a plain local directory

	syncMu sync.Mutex // serialises Sync
	mu     sync.RWMutex
	apps   map[string]*App
	sorted []*App
	loaded time.Time
}

// New returns a catalog stored in dir and fetched from repo.
func New(dir, repo string) *Catalog {
	return &Catalog{dir: dir, repo: repo, apps: map[string]*App{}}
}

// Dir returns the template directory of app id.
func (c *Catalog) Dir(id string) string { return filepath.Join(c.dir, id) }

// Sync clones or fast-forwards the catalog repository, then reloads it.
func (c *Catalog) Sync(ctx context.Context, out func(string)) error {
	c.syncMu.Lock()
	defer c.syncMu.Unlock()
	if c.repo == "" {
		return c.Load()
	}
	if c.checkedOut(ctx) {
		if err := git(ctx, out, "-C", c.dir, "pull", "--ff-only", "--depth", "1"); err != nil {
			return err
		}
	} else {
		// No checkout, or one an interrupted clone left without a commit:
		// start over.
		if err := os.MkdirAll(filepath.Dir(c.dir), 0o755); err != nil {
			return err
		}
		if err := os.RemoveAll(c.dir); err != nil {
			return err
		}
		if err := git(ctx, out, "clone", "--depth", "1", "--progress", c.repo, c.dir); err != nil {
			return err
		}
	}
	if err := c.Load(); err != nil {
		return err
	}
	if out != nil {
		out(fmt.Sprintf("Loaded %d apps", c.Len()))
	}
	return nil
}

// checkedOut reports whether dir holds a git checkout with a commit.
func (c *Catalog) checkedOut(ctx context.Context) bool {
	if _, err := os.Stat(filepath.Join(c.dir, ".git")); err != nil {
		return false
	}
	return exec.CommandContext(ctx, "git", "-C", c.dir, "rev-parse", "--verify", "--quiet", "HEAD").Run() == nil
}

func git(ctx context.Context, out func(string), args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.CombinedOutput()
	if out != nil {
		// git progress uses carriage returns; report the final state of each line.
		for line := range strings.SplitSeq(string(output), "\n") {
			if i := strings.LastIndex(line, "\r"); i >= 0 {
				line = line[i+1:]
			}
			if line = strings.TrimSpace(line); line != "" {
				out(line)
			}
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("git %s: %w", args[0], err)
	}
	return nil
}

// Load (re)reads every template from disk. Invalid templates are skipped.
func (c *Catalog) Load() error {
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return fmt.Errorf("catalog: %w", err)
	}
	apps := make(map[string]*App, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		app, err := c.readApp(entry.Name())
		if err != nil {
			continue
		}
		apps[app.ID] = app
	}
	if len(apps) == 0 {
		return fmt.Errorf("catalog: no valid apps in %s", c.dir)
	}
	sorted := make([]*App, 0, len(apps))
	for _, app := range apps {
		sorted = append(sorted, app)
	}
	slices.SortFunc(sorted, func(a, b *App) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	c.mu.Lock()
	c.apps, c.sorted, c.loaded = apps, sorted, time.Now()
	c.mu.Unlock()
	return nil
}

func (c *Catalog) readApp(id string) (*App, error) {
	dir := c.Dir(id)
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s/manifest.json: %w", id, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "docker-compose.yml")); err != nil {
		return nil, err
	}
	app := m.App
	app.ID = id // the directory name is authoritative
	app.available = m.Available
	app.icon = filepath.Base(m.Icon)
	if app.Name == "" {
		app.Name = id
	}
	return &app, nil
}

// Len returns the number of loaded apps.
func (c *Catalog) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.apps)
}

// LoadedAt returns when the catalog was last loaded (zero if never).
func (c *Catalog) LoadedAt() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.loaded
}

// Query filters the app list.
type Query struct {
	Search   string // matched against name, id and descriptions
	Category string
}

// List returns available, non-deprecated apps matching q, sorted by name.
func (c *Catalog) List(q Query) []*App {
	c.mu.RLock()
	defer c.mu.RUnlock()
	search := strings.ToLower(strings.TrimSpace(q.Search))
	out := make([]*App, 0, len(c.sorted))
	for _, app := range c.sorted {
		if !app.available || app.Deprecated {
			continue
		}
		if q.Category != "" && !slices.Contains(app.Categories, q.Category) {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(app.Name+"\n"+app.ID+"\n"+app.ShortDesc+"\n"+app.Description), search) {
			continue
		}
		out = append(out, app)
	}
	return out
}

// Get returns an app by id, including unavailable ones so installed apps can
// still be managed.
func (c *Catalog) Get(id string) (*App, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if app, ok := c.apps[id]; ok {
		return app, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
}

// Category is a category with the number of apps in it.
type Category struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// Categories returns the categories of listed apps, sorted by name.
func (c *Catalog) Categories() []Category {
	counts := map[string]int{}
	for _, app := range c.List(Query{}) {
		for _, name := range app.Categories {
			counts[name]++
		}
	}
	out := make([]Category, 0, len(counts))
	for name, count := range counts {
		out = append(out, Category{Name: name, Count: count})
	}
	slices.SortFunc(out, func(a, b Category) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// IconPath returns the path of the app's icon file.
func (c *Catalog) IconPath(id string) (string, error) {
	app, err := c.Get(id)
	if err != nil {
		return "", err
	}
	if app.icon == "" || app.icon == "." {
		return "", fmt.Errorf("%w: %s has no icon", ErrNotFound, id)
	}
	return filepath.Join(c.Dir(id), app.icon), nil
}

// Readme returns the app's README.md, or an empty string if it has none.
func (c *Catalog) Readme(id string) (string, error) {
	if _, err := c.Get(id); err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(c.Dir(id), "README.md"))
	if os.IsNotExist(err) {
		return "", nil
	}
	return string(data), err
}

// ComposeFile returns the compose template for arch, preferring an
// architecture-specific variant.
func (c *Catalog) ComposeFile(id, arch string) ([]byte, error) {
	if _, err := c.Get(id); err != nil {
		return nil, err
	}
	dir := c.Dir(id)
	if data, err := os.ReadFile(filepath.Join(dir, "docker-compose."+arch+".yml")); err == nil {
		return data, nil
	}
	return os.ReadFile(filepath.Join(dir, "docker-compose.yml"))
}

// templateFiles are template entries that describe the app rather than seed
// its data directory.
func isTemplateFile(name string) bool {
	switch name {
	case "manifest.json", "README.md", "metadata":
		return true
	}
	return strings.HasPrefix(name, "docker-compose") || strings.HasPrefix(name, "logo.")
}

// SeedEntries lists the top-level files and directories of the template that
// must be copied into an installed app's directory (e.g. data/, config/).
func (c *Catalog) SeedEntries(id string) ([]string, error) {
	entries, err := os.ReadDir(c.Dir(id))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		if !isTemplateFile(entry.Name()) && !strings.HasPrefix(entry.Name(), ".") {
			out = append(out, entry.Name())
		}
	}
	return out, nil
}
