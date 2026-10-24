package api

import (
	"context"
	"net/http"
	"os"

	"github.com/lsongdev/docker-go/apps"
	"github.com/lsongdev/docker-go/catalog"
	"github.com/lsongdev/docker-go/jobs"
)

// storeApp is a catalog entry annotated with local install state.
type storeApp struct {
	*catalog.App
	Installed bool `json:"installed"`
}

func (s *Server) listCatalog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list := s.Catalog.List(catalog.Query{Search: q.Get("search"), Category: q.Get("category")})
	out := make([]storeApp, 0, len(list))
	for _, app := range list {
		out = append(out, storeApp{App: app, Installed: s.Apps.IsInstalled(app.ID)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": out, "loaded_at": s.Catalog.LoadedAt()})
}

func (s *Server) catalogCategories(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Catalog.Categories())
}

func (s *Server) getCatalogApp(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	app, err := s.Catalog.Get(id)
	if err != nil {
		fail(w, err)
		return
	}
	readme, err := s.Catalog.Readme(id)
	if err != nil {
		fail(w, err)
		return
	}
	arch, err := s.Apps.Arch(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"app": storeApp{App: app, Installed: s.Apps.IsInstalled(id)}, "readme": readme,
		"arch": arch, "supported": app.SupportsArch(arch),
	})
}

func (s *Server) catalogIcon(w http.ResponseWriter, r *http.Request) {
	path, err := s.Catalog.IconPath(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "icon not found")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox") // icons may be SVG
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

func (s *Server) syncCatalog(w http.ResponseWriter, r *http.Request) {
	s.startJob(w, jobs.Spec{Kind: "catalog.sync", Target: "catalog", Title: "Sync app catalog"}, func(ctx context.Context, log func(string)) error {
		return s.Catalog.Sync(ctx, log)
	})
}

func (s *Server) listApps(w http.ResponseWriter, r *http.Request) {
	list, err := s.Apps.List(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) getApp(w http.ResponseWriter, r *http.Request) {
	app, err := s.Apps.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, app)
}

type installRequest struct {
	ID string `json:"id"`
	apps.Request
}

// installApp validates synchronously (422 with per-field errors) and then
// installs in the background.
func (s *Server) installApp(w http.ResponseWriter, r *http.Request) {
	var req installRequest
	if err := decode(w, r, &req); err != nil {
		fail(w, err)
		return
	}
	plan, err := s.Apps.PlanInstall(r.Context(), req.ID, req.Request)
	if err != nil {
		fail(w, err)
		return
	}
	s.startJob(w, jobs.Spec{Kind: "app.install", Target: "app:" + req.ID, Title: "Install " + plan.App.Name}, func(ctx context.Context, log func(string)) error {
		return s.Apps.Apply(ctx, plan, log)
	})
}

// updateApp re-renders an installed app from the current catalog template,
// optionally changing its port or settings.
func (s *Server) updateApp(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req apps.Request
	if r.ContentLength != 0 {
		if err := decode(w, r, &req); err != nil {
			fail(w, err)
			return
		}
	}
	plan, err := s.Apps.PlanUpdate(r.Context(), id, req)
	if err != nil {
		fail(w, err)
		return
	}
	s.startJob(w, jobs.Spec{Kind: "app.update", Target: "app:" + id, Title: "Update " + plan.App.Name}, func(ctx context.Context, log func(string)) error {
		return s.Apps.Apply(ctx, plan, log)
	})
}

func (s *Server) uninstallApp(w http.ResponseWriter, r *http.Request) {
	id, keepData := r.PathValue("id"), queryBool(r, "keep_data")
	if !s.Apps.IsInstalled(id) {
		fail(w, apps.ErrNotInstalled)
		return
	}
	s.startJob(w, jobs.Spec{Kind: "app.uninstall", Target: "app:" + id, Title: "Uninstall " + id}, func(ctx context.Context, log func(string)) error {
		return s.Apps.Uninstall(ctx, id, keepData, log)
	})
}

func (s *Server) appAction(w http.ResponseWriter, r *http.Request) {
	id, action := r.PathValue("id"), apps.Action(r.PathValue("action"))
	switch action {
	case apps.ActionStart, apps.ActionStop, apps.ActionRestart:
	default:
		writeError(w, http.StatusNotFound, "not_found", "unknown app action")
		return
	}
	if !s.Apps.IsInstalled(id) {
		fail(w, apps.ErrNotInstalled)
		return
	}
	s.startJob(w, jobs.Spec{Kind: "app." + string(action), Target: "app:" + id, Title: string(action) + " " + id}, func(ctx context.Context, log func(string)) error {
		return s.Apps.Do(ctx, id, action, log)
	})
}
