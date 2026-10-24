package api

import (
	"context"
	"net/http"
	"time"

	"github.com/lsongdev/docker-go/engine"
)

type systemDTO struct {
	Host           string    `json:"host"`
	APIVersion     string    `json:"api_version"`
	Version        string    `json:"version"`
	OS             string    `json:"os"`
	Arch           string    `json:"arch"`
	Kernel         string    `json:"kernel"`
	Name           string    `json:"name"`
	CPUs           int       `json:"cpus"`
	Memory         int64     `json:"memory"`
	StorageDriver  string    `json:"storage_driver"`
	RootDir        string    `json:"root_dir"`
	Containers     int       `json:"containers"`
	Running        int       `json:"running"`
	Paused         int       `json:"paused"`
	Stopped        int       `json:"stopped"`
	Images         int       `json:"images"`
	ComposeVersion string    `json:"compose_version"`
	CatalogApps    int       `json:"catalog_apps"`
	CatalogLoaded  time.Time `json:"catalog_loaded_at"`
}

func (s *Server) system(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	info, err := s.Engine.Info(ctx)
	if err != nil {
		fail(w, err)
		return
	}
	version, err := s.Engine.APIVersion(ctx)
	if err != nil {
		fail(w, err)
		return
	}
	composeVersion, _ := s.Stacks.CLI().Version(ctx)
	writeJSON(w, http.StatusOK, systemDTO{
		Host: s.Engine.Host(), APIVersion: version, Version: info.ServerVersion, OS: info.OperatingSystem,
		Arch: info.Architecture, Kernel: info.KernelVersion, Name: info.Name, CPUs: info.NCPU, Memory: info.MemTotal,
		StorageDriver: info.Driver, RootDir: info.DockerRootDir, Containers: info.Containers,
		Running: info.ContainersRunning, Paused: info.ContainersPaused, Stopped: info.ContainersStopped,
		Images: info.Images, ComposeVersion: composeVersion,
		CatalogApps: s.Catalog.Len(), CatalogLoaded: s.Catalog.LoadedAt(),
	})
}

// eventDTO is a trimmed daemon event, enough for clients to refresh views.
type eventDTO struct {
	Type   string `json:"type"`
	Action string `json:"action"`
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	Time   int64  `json:"time"`
}

// events streams daemon events about containers, images, volumes and
// networks as SSE "event" messages.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	// Check connectivity first so a dead daemon yields a proper error.
	if _, err := s.Engine.APIVersion(ctx); err != nil {
		fail(w, err)
		return
	}
	events := make(chan engine.Event, 64)
	errc := make(chan error, 1)
	go func() {
		filters := engine.Filters{"type": {"container", "image", "volume", "network"}}
		errc <- s.Engine.Events(ctx, filters, func(e engine.Event) error {
			select {
			case events <- e:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	stream := newSSE(w)
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	for {
		select {
		case e := <-events:
			dto := eventDTO{Type: e.Type, Action: e.Action, ID: e.Actor.ID, Name: e.Actor.Attributes["name"], Time: e.Time}
			if stream.send("event", dto) != nil {
				return
			}
		case err := <-errc:
			if err != nil && ctx.Err() == nil {
				stream.send("error", map[string]string{"message": err.Error()})
			}
			return
		case <-ticker.C:
			if stream.ping() != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
