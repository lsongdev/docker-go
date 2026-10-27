package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"
	"time"

	"github.com/lsongdev/docker-go/apps"
	"github.com/lsongdev/docker-go/catalog"
	"github.com/lsongdev/docker-go/compose"
	"github.com/lsongdev/docker-go/engine"
	"github.com/lsongdev/docker-go/jobs"
)

const maxBodyBytes = 4 << 20 // compose files can be large; nothing else is

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"data": data})
}

type apiError struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeAPIError(w, status, apiError{Code: code, Message: message})
}

func writeAPIError(w http.ResponseWriter, status int, e apiError) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"error": e})
}

// badRequest marks a client mistake detected by the API layer itself.
type badRequest struct{ msg string }

func (e badRequest) Error() string { return e.msg }

func invalid(format string, args ...any) error { return badRequest{fmt.Sprintf(format, args...)} }

// fail maps a domain error to an HTTP response.
func fail(w http.ResponseWriter, err error) {
	var fields catalog.FieldErrors
	var bad badRequest
	var engineErr *engine.Error
	switch {
	case errors.As(err, &fields):
		writeAPIError(w, http.StatusUnprocessableEntity, apiError{Code: "validation_failed", Message: "Some settings are invalid", Fields: fields})
	case errors.As(err, &bad):
		writeError(w, http.StatusBadRequest, "invalid_request", bad.msg)
	case errors.Is(err, compose.ErrInvalid), errors.Is(err, apps.ErrUnavailable):
		writeError(w, http.StatusUnprocessableEntity, "validation_failed", err.Error())
	case errors.Is(err, catalog.ErrNotFound), errors.Is(err, compose.ErrNotFound),
		errors.Is(err, apps.ErrNotInstalled), errors.Is(err, jobs.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, compose.ErrExists), errors.Is(err, apps.ErrInstalled),
		errors.Is(err, compose.ErrReadOnly), errors.Is(err, jobs.ErrBusy):
		writeError(w, http.StatusConflict, "conflict", err.Error())
	case errors.As(err, &engineErr):
		switch {
		case engineErr.StatusCode == http.StatusNotFound:
			writeError(w, http.StatusNotFound, "not_found", engineErr.Message)
		case engineErr.StatusCode == http.StatusConflict:
			writeError(w, http.StatusConflict, "conflict", engineErr.Message)
		case engineErr.StatusCode < 500:
			writeError(w, http.StatusBadRequest, "docker_rejected", engineErr.Message)
		default:
			writeError(w, http.StatusBadGateway, "docker_error", engineErr.Message)
		}
	case engine.NotInstalled(err):
		writeError(w, http.StatusServiceUnavailable, "docker_not_installed", "Docker is not installed on this server")
	case errors.Is(err, engine.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "docker_unavailable", err.Error())
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusServiceUnavailable, "canceled", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
	}
}

// decode reads a JSON body strictly: correct content type, bounded size,
// no unknown fields and no trailing data.
func decode(w http.ResponseWriter, r *http.Request, dst any) error {
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "application/json" {
		return invalid("Content-Type must be application/json")
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return invalid("invalid JSON body: %v", err)
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return invalid("unexpected data after JSON body")
	}
	return nil
}

func queryBool(r *http.Request, key string) bool {
	v, _ := strconv.ParseBool(r.URL.Query().Get(key))
	return v
}

// sse writes a server-sent event stream.
type sse struct {
	w  http.ResponseWriter
	rc *http.ResponseController
}

func newSSE(w http.ResponseWriter) *sse {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no") // disable proxy buffering (nginx)
	w.WriteHeader(http.StatusOK)
	s := &sse{w: w, rc: http.NewResponseController(w)}
	s.rc.Flush()
	return s
}

func (s *sse) send(event string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, payload); err != nil {
		return err
	}
	return s.rc.Flush()
}

// ping keeps idle connections open through proxies.
func (s *sse) ping() error {
	if _, err := io.WriteString(s.w, ": ping\n\n"); err != nil {
		return err
	}
	return s.rc.Flush()
}

// heartbeat is how often idle streams are pinged. An sse is not safe for
// concurrent use, so streaming handlers select on a ticker in the same loop
// that sends their events.
const heartbeat = 20 * time.Second
