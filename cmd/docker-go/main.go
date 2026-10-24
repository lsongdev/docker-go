// Command docker-go serves the Docker management panel.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/lsongdev/docker-go/api"
	"github.com/lsongdev/docker-go/apps"
	"github.com/lsongdev/docker-go/catalog"
	"github.com/lsongdev/docker-go/compose"
	"github.com/lsongdev/docker-go/engine"
	"github.com/lsongdev/docker-go/jobs"
	"github.com/lsongdev/docker-go/web"
)

const defaultCatalog = "https://github.com/lsongdev/ohmyserver-apps.git"

func main() {
	listen := flag.String("listen", "127.0.0.1:8080", "HTTP listen address")
	dataDir := flag.String("data", "./data", "directory for stacks, apps and the catalog")
	catalogRepo := flag.String("catalog", defaultCatalog, "app catalog git repository (empty: use <data>/catalog as is)")
	host := flag.String("host", "", "address users reach installed apps at (default: derived from -listen)")
	storage := flag.String("storage", "", "shared storage directory exposed to apps (default: <data>/storage)")
	tz := flag.String("tz", "", "timezone passed to apps (default: system timezone)")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)
	if err := run(*listen, *dataDir, *catalogRepo, *host, *storage, *tz, logger); err != nil {
		logger.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(listen, dataDir, catalogRepo, host, storage, tz string, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return err
	}
	if storage == "" {
		storage = filepath.Join(dataDir, "storage")
	}
	for _, dir := range []string{dataDir, filepath.Join(dataDir, "stacks"), filepath.Join(dataDir, "apps"), storage} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	eng, err := engine.FromEnv()
	if err != nil {
		return err
	}
	defer eng.Close()
	if version, err := eng.APIVersion(ctx); err != nil {
		logger.Warn("docker daemon unavailable; the panel will report errors until it is reachable", "host", eng.Host(), "err", err)
	} else {
		logger.Info("connected to docker", "host", eng.Host(), "api", version)
	}
	cli, err := compose.NewCLI()
	if err != nil {
		return err
	}

	// Jobs outlive requests but stop with the process.
	jobCtx, cancelJobs := context.WithCancel(context.Background())
	defer cancelJobs()
	jobManager := jobs.NewManager(jobCtx, 100)

	cat := catalog.New(filepath.Join(dataDir, "catalog"), catalogRepo)
	if err := cat.Load(); err != nil {
		logger.Info("app catalog not loaded yet; syncing", "repo", catalogRepo)
		jobManager.Start(jobs.Spec{Kind: "catalog.sync", Target: "catalog", Title: "Sync app catalog"}, cat.Sync)
	}

	if host == "" {
		host = defaultHost(listen)
	}
	appManager := apps.NewManager(filepath.Join(dataDir, "apps"), cat, cli, eng, apps.Settings{
		Host: host, Timezone: firstNonEmpty(tz, systemTimezone()), StorageDir: storage,
	})

	server := &api.Server{
		Engine: eng, Stacks: compose.NewStore(filepath.Join(dataDir, "stacks"), cli),
		Catalog: cat, Apps: appManager, Jobs: jobManager, Logger: logger,
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", http.StripPrefix("/api", server.Handler()))
	mux.Handle("/", web.Handler())

	srv := &http.Server{
		Addr:              listen,
		Handler:           accessLog(logger, mux),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	logger.Info("panel listening", "url", "http://"+ln.Addr().String(), "data", dataDir, "apps_host", host)
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		logger.Warn("shutdown", "err", err)
	}
	cancelJobs()
	done := make(chan struct{})
	go func() { jobManager.Wait(); close(done) }()
	select {
	case <-done:
	case <-shutdownCtx.Done():
		logger.Warn("some jobs did not stop in time")
	}
	return nil
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

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
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
			logger.Info("request", "method", r.Method, "path", r.URL.Path, "status", rec.status, "duration", fmt.Sprint(time.Since(start).Round(time.Millisecond)))
		}
	})
}
