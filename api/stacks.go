package api

import (
	"context"
	"net/http"

	"github.com/lsongdev/docker-go/compose"
	"github.com/lsongdev/docker-go/jobs"
)

func (s *Server) listStacks(w http.ResponseWriter, r *http.Request) {
	stacks, err := s.Stacks.List(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stacks)
}

func (s *Server) getStack(w http.ResponseWriter, r *http.Request) {
	stack, err := s.Stacks.Get(r.Context(), r.PathValue("name"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, stack)
}

type createStackRequest struct {
	Name string `json:"name"`
	compose.Spec
}

// createStack stores and validates a stack without starting it; clients
// follow up with POST /stacks/{name}/up.
func (s *Server) createStack(w http.ResponseWriter, r *http.Request) {
	var req createStackRequest
	if err := decode(w, r, &req); err != nil {
		fail(w, err)
		return
	}
	stack, err := s.Stacks.Create(r.Context(), req.Name, req.Spec)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, stack)
}

func (s *Server) updateStack(w http.ResponseWriter, r *http.Request) {
	var spec compose.Spec
	if err := decode(w, r, &spec); err != nil {
		fail(w, err)
		return
	}
	if err := s.Stacks.Update(r.Context(), r.PathValue("name"), spec); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) deleteStack(w http.ResponseWriter, r *http.Request) {
	name, volumes := r.PathValue("name"), queryBool(r, "volumes")
	if _, err := s.Stacks.Get(r.Context(), name); err != nil {
		fail(w, err)
		return
	}
	s.startJob(w, jobs.Spec{Kind: "stack.delete", Target: "stack:" + name, Title: "Delete stack " + name}, func(ctx context.Context, log func(string)) error {
		return s.Stacks.Delete(ctx, name, volumes, log)
	})
}

func (s *Server) stackAction(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	action, err := compose.ParseAction(r.PathValue("action"))
	if err != nil {
		fail(w, err)
		return
	}
	if _, err := s.Stacks.Get(r.Context(), name); err != nil {
		fail(w, err)
		return
	}
	s.startJob(w, jobs.Spec{Kind: "stack." + string(action), Target: "stack:" + name, Title: "Stack " + name + ": " + string(action)}, func(ctx context.Context, log func(string)) error {
		return s.Stacks.Do(ctx, name, action, log)
	})
}
