package api

import (
	"net/http"
	"time"

	"github.com/lsongdev/docker-go/jobs"
)

// startJob runs fn as a background job and answers 202 with the job.
func (s *Server) startJob(w http.ResponseWriter, spec jobs.Spec, fn jobs.Func) {
	snap, err := s.Jobs.Start(spec, fn)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, snap)
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Jobs.List())
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	snap, lines, err := s.Jobs.Get(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": snap, "lines": lines})
}

func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request) {
	if err := s.Jobs.Cancel(r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// jobEvents streams a job: one "snapshot" event with the log so far, a
// "line" event per new line, and a final "done" event with the job state.
func (s *Server) jobEvents(w http.ResponseWriter, r *http.Request) {
	snap, _, err := s.Jobs.Get(r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	lines, events, err := s.Jobs.Watch(r.Context(), snap.ID)
	if err != nil {
		fail(w, err)
		return
	}
	stream := newSSE(w)
	if stream.send("snapshot", map[string]any{"job": snap, "lines": lines}) != nil {
		return
	}
	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	for {
		select {
		case event, ok := <-events:
			if !ok {
				return
			}
			if event.Done != nil {
				stream.send("done", event.Done)
				return
			}
			if stream.send("line", event.Line) != nil {
				return
			}
		case <-ticker.C:
			if stream.ping() != nil {
				return
			}
		}
	}
}
