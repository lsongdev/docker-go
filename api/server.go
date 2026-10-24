// Package api exposes the panel over HTTP.
//
// All routes are relative to wherever the handler is mounted. Responses use
// a JSON envelope: {"data": ...} on success and
// {"error": {"code", "message", "fields"}} on failure. Long operations run as
// jobs: the endpoint answers 202 with the job, whose output can be followed
// at /jobs/{id}/events. Live data (logs, stats, events, job output) is
// streamed with server-sent events; the container terminal uses WebSocket.
package api

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/lsongdev/docker-go/apps"
	"github.com/lsongdev/docker-go/catalog"
	"github.com/lsongdev/docker-go/compose"
	"github.com/lsongdev/docker-go/engine"
	"github.com/lsongdev/docker-go/jobs"
)

// Server holds the services the API is built on.
type Server struct {
	Engine  *engine.Client
	Stacks  *compose.Store
	Catalog *catalog.Catalog
	Apps    *apps.Manager
	Jobs    *jobs.Manager
	Logger  *slog.Logger
}

// Handler returns the API handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /system", s.system)
	mux.HandleFunc("GET /events", s.events)

	mux.HandleFunc("GET /containers", s.listContainers)
	mux.HandleFunc("POST /containers", s.createContainer)
	mux.HandleFunc("POST /containers/prune", s.pruneContainers)
	mux.HandleFunc("GET /containers/{id}", s.getContainer)
	mux.HandleFunc("DELETE /containers/{id}", s.removeContainer)
	mux.HandleFunc("POST /containers/{id}/{action}", s.containerAction)
	mux.HandleFunc("GET /containers/{id}/logs", s.containerLogs)
	mux.HandleFunc("GET /containers/{id}/stats", s.containerStats)
	mux.HandleFunc("GET /containers/{id}/terminal", s.containerTerminal)

	mux.HandleFunc("GET /images", s.listImages)
	mux.HandleFunc("POST /images/pull", s.pullImage)
	mux.HandleFunc("POST /images/build", s.buildImage)
	mux.HandleFunc("POST /images/prune", s.pruneImages)
	// Image references may contain slashes (library/nginx, ghcr.io/o/app:v1).
	mux.HandleFunc("GET /images/{id...}", s.getImage)
	mux.HandleFunc("DELETE /images/{id...}", s.removeImage)

	mux.HandleFunc("GET /volumes", s.listVolumes)
	mux.HandleFunc("POST /volumes", s.createVolume)
	mux.HandleFunc("POST /volumes/prune", s.pruneVolumes)
	mux.HandleFunc("GET /volumes/{name}", s.getVolume)
	mux.HandleFunc("DELETE /volumes/{name}", s.removeVolume)

	mux.HandleFunc("GET /networks", s.listNetworks)
	mux.HandleFunc("POST /networks", s.createNetwork)
	mux.HandleFunc("POST /networks/prune", s.pruneNetworks)
	mux.HandleFunc("GET /networks/{id}", s.getNetwork)
	mux.HandleFunc("DELETE /networks/{id}", s.removeNetwork)

	mux.HandleFunc("GET /stacks", s.listStacks)
	mux.HandleFunc("POST /stacks", s.createStack)
	mux.HandleFunc("GET /stacks/{name}", s.getStack)
	mux.HandleFunc("PUT /stacks/{name}", s.updateStack)
	mux.HandleFunc("DELETE /stacks/{name}", s.deleteStack)
	mux.HandleFunc("POST /stacks/{name}/{action}", s.stackAction)

	mux.HandleFunc("GET /catalog", s.listCatalog)
	mux.HandleFunc("GET /catalog/categories", s.catalogCategories)
	mux.HandleFunc("POST /catalog/sync", s.syncCatalog)
	mux.HandleFunc("GET /catalog/{id}", s.getCatalogApp)
	mux.HandleFunc("GET /catalog/{id}/icon", s.catalogIcon)

	mux.HandleFunc("GET /apps", s.listApps)
	mux.HandleFunc("POST /apps", s.installApp)
	mux.HandleFunc("GET /apps/{id}", s.getApp)
	mux.HandleFunc("PATCH /apps/{id}", s.updateApp)
	mux.HandleFunc("DELETE /apps/{id}", s.uninstallApp)
	mux.HandleFunc("POST /apps/{id}/{action}", s.appAction)

	mux.HandleFunc("GET /jobs", s.listJobs)
	mux.HandleFunc("GET /jobs/{id}", s.getJob)
	mux.HandleFunc("GET /jobs/{id}/events", s.jobEvents)
	mux.HandleFunc("POST /jobs/{id}/cancel", s.cancelJob)

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "API endpoint not found")
	})

	// Every state-changing request must come from the panel's own origin.
	csrf := http.NewCrossOriginProtection()
	return s.recover(csrf.Handler(mux))
}

func (s *Server) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

func (s *Server) recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				s.logger().Error("api panic", "method", r.Method, "path", r.URL.Path, "panic", v, "stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
