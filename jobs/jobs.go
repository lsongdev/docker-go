// Package jobs runs long operations (installs, pulls, builds, compose
// actions) in the background and fans their output out to any number of
// observers.
//
// Jobs outlive the request that started them, so a client can navigate away
// and reattach to the log later. History is kept in memory and bounded.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// ErrBusy is returned when another job already holds the same target.
var ErrBusy = errors.New("another operation is already running on this resource")

var ErrNotFound = errors.New("job not found")

type Status string

const (
	Running   Status = "running"
	Succeeded Status = "succeeded"
	Failed    Status = "failed"
	Canceled  Status = "canceled"
)

// Spec describes a job to start.
type Spec struct {
	Kind   string // machine-readable kind, e.g. "app.install"
	Target string // resource key; at most one running job per target
	Title  string // human-readable description
}

// Func is the job body. It reports progress through log.
type Func func(ctx context.Context, log func(string)) error

// Snapshot is a point-in-time copy of a job.
type Snapshot struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"`
	Target     string     `json:"target"`
	Title      string     `json:"title"`
	Status     Status     `json:"status"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Lines      int        `json:"lines"`
}

// Event is delivered to observers: either a log line or the final state.
type Event struct {
	Line string    `json:"line,omitempty"`
	Done *Snapshot `json:"done,omitempty"`
}

const maxLines = 5000

type job struct {
	mu       sync.Mutex
	snap     Snapshot
	lines    []string // the most recent maxLines lines
	watchers map[chan Event]struct{}
	cancel   context.CancelFunc
}

// Manager owns all jobs. Jobs run under the manager's context, not the
// context of the request that started them.
type Manager struct {
	ctx     context.Context
	keep    int
	mu      sync.Mutex
	jobs    map[string]*job
	order   []string // oldest first
	targets map[string]string
	wg      sync.WaitGroup
}

// NewManager creates a manager that keeps the last keep finished jobs.
func NewManager(ctx context.Context, keep int) *Manager {
	return &Manager{ctx: ctx, keep: keep, jobs: map[string]*job{}, targets: map[string]string{}}
}

// Start runs fn in the background. It fails with ErrBusy if a job for the
// same target is still running.
func (m *Manager) Start(spec Spec, fn Func) (Snapshot, error) {
	m.mu.Lock()
	if spec.Target != "" {
		if id, ok := m.targets[spec.Target]; ok {
			m.mu.Unlock()
			return Snapshot{}, fmt.Errorf("%w (job %s)", ErrBusy, id)
		}
	}
	ctx, cancel := context.WithCancel(m.ctx)
	j := &job{
		snap:     Snapshot{ID: newID(), Kind: spec.Kind, Target: spec.Target, Title: spec.Title, Status: Running, StartedAt: time.Now()},
		watchers: map[chan Event]struct{}{},
		cancel:   cancel,
	}
	m.jobs[j.snap.ID] = j
	m.order = append(m.order, j.snap.ID)
	if spec.Target != "" {
		m.targets[spec.Target] = j.snap.ID
	}
	m.prune()
	snap := j.snap
	m.mu.Unlock()

	m.wg.Go(func() {
		defer cancel()
		err := run(ctx, fn, j.log)
		m.mu.Lock()
		if spec.Target != "" && m.targets[spec.Target] == snap.ID {
			delete(m.targets, spec.Target)
		}
		m.mu.Unlock()
		j.finish(ctx, err)
	})
	return snap, nil
}

// run calls fn, converting a panic into a job failure.
func run(ctx context.Context, fn Func, log func(string)) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return fn(ctx, log)
}

// prune drops the oldest finished jobs beyond the retention limit. m.mu must
// be held.
func (m *Manager) prune() {
	finished := 0
	for _, id := range m.order {
		if m.jobs[id].status() != Running {
			finished++
		}
	}
	for i := 0; finished > m.keep && i < len(m.order); {
		id := m.order[i]
		if m.jobs[id].status() == Running {
			i++
			continue
		}
		delete(m.jobs, id)
		m.order = slices.Delete(m.order, i, i+1)
		finished--
	}
}

// Wait blocks until every running job has finished.
func (m *Manager) Wait() { m.wg.Wait() }

// List returns all retained jobs, newest first.
func (m *Manager) List() []Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Snapshot, 0, len(m.order))
	for _, id := range slices.Backward(m.order) {
		out = append(out, m.jobs[id].snapshot())
	}
	return out
}

func (m *Manager) get(id string) (*job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[id]; ok {
		return j, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
}

// Get returns a job snapshot and its retained log lines.
func (m *Manager) Get(id string) (Snapshot, []string, error) {
	j, err := m.get(id)
	if err != nil {
		return Snapshot{}, nil, err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.snap, slices.Clone(j.lines), nil
}

// Running returns the running job for target, if any.
func (m *Manager) Running(target string) (Snapshot, bool) {
	m.mu.Lock()
	id, ok := m.targets[target]
	j := m.jobs[id]
	m.mu.Unlock()
	if !ok || j == nil {
		return Snapshot{}, false
	}
	return j.snapshot(), true
}

// Cancel requests cancellation of a running job.
func (m *Manager) Cancel(id string) error {
	j, err := m.get(id)
	if err != nil {
		return err
	}
	j.cancel()
	return nil
}

// Watch returns the log so far and a channel of subsequent events. The
// channel is closed after the final Done event, or when ctx ends. If the job
// already finished, the channel delivers only Done.
func (m *Manager) Watch(ctx context.Context, id string) ([]string, <-chan Event, error) {
	j, err := m.get(id)
	if err != nil {
		return nil, nil, err
	}
	ch := make(chan Event, 256)
	j.mu.Lock()
	lines := slices.Clone(j.lines)
	if j.snap.Status != Running {
		snap := j.snap
		j.mu.Unlock()
		ch <- Event{Done: &snap}
		close(ch)
		return lines, ch, nil
	}
	j.watchers[ch] = struct{}{}
	j.mu.Unlock()
	go func() {
		<-ctx.Done()
		j.mu.Lock()
		if _, ok := j.watchers[ch]; ok {
			delete(j.watchers, ch)
			close(ch)
		}
		j.mu.Unlock()
	}()
	return lines, ch, nil
}

func (j *job) status() Status {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.snap.Status
}

func (j *job) snapshot() Snapshot {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.snap
}

func (j *job) log(line string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.lines = append(j.lines, line)
	if len(j.lines) > maxLines {
		j.lines = j.lines[len(j.lines)-maxLines:]
	}
	j.snap.Lines++
	for ch := range j.watchers {
		select {
		case ch <- Event{Line: line}:
		default: // a slow observer misses lines rather than stalling the job
		}
	}
}

func (j *job) finish(ctx context.Context, err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	now := time.Now()
	j.snap.FinishedAt = &now
	switch {
	case err == nil:
		j.snap.Status = Succeeded
	case errors.Is(ctx.Err(), context.Canceled):
		j.snap.Status = Canceled
		j.snap.Error = "canceled"
	default:
		j.snap.Status = Failed
		j.snap.Error = err.Error()
	}
	snap := j.snap
	for ch := range j.watchers {
		// Done must not be dropped: wait briefly for slow observers.
		select {
		case ch <- Event{Done: &snap}:
		case <-time.After(time.Second):
		}
		close(ch)
	}
	j.watchers = nil
}

func newID() string {
	var b [8]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
