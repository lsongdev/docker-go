// Package web assembles the Docker module: it wires the engine, compose,
// catalog, apps, jobs and storage layers into the HTTP API and pairs it with
// the browser UI (native ES modules in ui/, no build step).
//
// A Docker value satisfies the panel module contract (see panelkit's
// CONTRACT.md) without depending on any panel: Handler serves the API with
// relative routes and Assets holds the UI with manifest.json at its root.
// The standalone command and a panel build it the same way, through New.
// It needs no database: install records are files beside each app's compose
// files unless the host supplies another apps.Store.
package web

import (
	"cmp"
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/lsongdev/docker-go/api"
	"github.com/lsongdev/docker-go/apps"
	"github.com/lsongdev/docker-go/catalog"
	"github.com/lsongdev/docker-go/compose"
	"github.com/lsongdev/docker-go/engine"
	"github.com/lsongdev/docker-go/jobs"
)

// DefaultCatalog is the app catalog used when Options.CatalogRepo is empty.
const DefaultCatalog = "https://github.com/lsongdev/ohmyserver-apps.git"

//go:embed ui
var embedded embed.FS

// UI is the module's browser assets; manifest.json describes them.
var UI = func() fs.FS {
	sub, err := fs.Sub(embedded, "ui")
	if err != nil {
		panic(err) // the embed directive guarantees the directory exists
	}
	return sub
}()

// Options configures the Docker module.
type Options struct {
	DataDir     string       // holds stacks/, apps/, catalog/ and, by default, storage/
	CatalogRepo string       // app catalog git URL; "-" uses <DataDir>/catalog as is
	AppsHost    string       // address users reach installed apps at
	StorageDir  string       // shared media root exposed to apps (default <DataDir>/storage)
	Timezone    string       // TZ passed to apps (default: the system timezone)
	Exposer     apps.Exposer // publishes apps at a domain; optional
	Store       apps.Store   // install records; app.json files by default
	Logger      *slog.Logger
}

// Docker is an assembled Docker module.
type Docker struct {
	Engine  *engine.Client
	Stacks  *compose.Store
	Catalog *catalog.Catalog
	Apps    *apps.Manager
	Jobs    *jobs.Manager
	Server  *api.Server

	cancelJobs context.CancelFunc
}

// New assembles the module. It does not require the Docker daemon to be
// reachable: requests report errors until it is.
func New(ctx context.Context, opts Options) (*Docker, error) {
	logger := cmp.Or(opts.Logger, slog.Default())
	dataDir, err := filepath.Abs(opts.DataDir)
	if err != nil {
		return nil, err
	}
	storage := cmp.Or(opts.StorageDir, filepath.Join(dataDir, "storage"))
	for _, dir := range []string{dataDir, filepath.Join(dataDir, "stacks"), filepath.Join(dataDir, "apps"), storage} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	appsDir := filepath.Join(dataDir, "apps")
	records := opts.Store
	if records == nil {
		records = apps.NewFileStore(appsDir)
	}
	eng, err := engine.FromEnv()
	if err != nil {
		return nil, err
	}
	if version, err := eng.APIVersion(ctx); err != nil {
		logger.Warn("docker daemon unavailable; requests will fail until it is reachable", "host", eng.Host(), "err", err)
	} else {
		logger.Info("connected to docker", "host", eng.Host(), "api", version)
	}
	cli, err := compose.NewCLI()
	if err != nil {
		logger.Warn("docker compose unavailable; stacks and apps fail until it is installed", "err", err)
		cli = &compose.CLI{}
	}

	// Jobs outlive requests; Close stops them.
	jobCtx, cancelJobs := context.WithCancel(context.WithoutCancel(ctx))
	jobManager := jobs.NewManager(jobCtx, 100)

	repo := cmp.Or(opts.CatalogRepo, DefaultCatalog)
	if repo == "-" {
		repo = ""
	}
	cat := catalog.New(filepath.Join(dataDir, "catalog"), repo)
	if err := cat.Load(); err != nil && repo != "" {
		logger.Info("app catalog not loaded yet; syncing", "repo", repo)
		jobManager.Start(jobs.Spec{Kind: "catalog.sync", Target: "catalog", Title: "Sync app catalog"}, cat.Sync)
	}

	appManager := apps.NewManager(appsDir, records, cat, cli, eng, apps.Settings{
		Host: opts.AppsHost, Timezone: cmp.Or(opts.Timezone, systemTimezone()), StorageDir: storage, Exposer: opts.Exposer,
	})
	d := &Docker{
		Engine: eng, Stacks: compose.NewStore(filepath.Join(dataDir, "stacks"), cli),
		Catalog: cat, Apps: appManager, Jobs: jobManager, cancelJobs: cancelJobs,
	}
	d.Server = &api.Server{Engine: eng, Stacks: d.Stacks, Catalog: cat, Apps: appManager, Jobs: jobManager, Logger: logger}
	return d, nil
}

// Handler serves the module's API with relative routes.
func (d *Docker) Handler() http.Handler { return d.Server.Handler() }

// Assets returns the browser UI; manifest.json is at its root.
func (d *Docker) Assets() fs.FS { return UI }

// Close cancels running jobs, waits for them and releases the daemon
// connection.
func (d *Docker) Close() error {
	d.cancelJobs()
	d.Jobs.Wait()
	return d.Engine.Close()
}

// systemTimezone returns the IANA name of the host timezone.
func systemTimezone() string {
	if tz := os.Getenv("TZ"); tz != "" {
		return tz
	}
	if data, err := os.ReadFile("/etc/timezone"); err == nil {
		if tz := strings.TrimSpace(string(data)); tz != "" {
			return tz
		}
	}
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if _, tz, ok := strings.Cut(target, "zoneinfo/"); ok {
			return tz
		}
	}
	return "UTC"
}
