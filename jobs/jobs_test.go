package jobs

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func waitDone(t *testing.T, m *Manager, id string) Snapshot {
	t.Helper()
	_, events, err := m.Watch(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	timeout := time.After(5 * time.Second)
	for {
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatal("stream closed without done")
			}
			if e.Done != nil {
				return *e.Done
			}
		case <-timeout:
			t.Fatal("job did not finish")
		}
	}
}

func TestJobLifecycle(t *testing.T) {
	m := NewManager(context.Background(), 10)
	release := make(chan struct{})
	snap, err := m.Start(Spec{Kind: "test", Target: "res", Title: "Test"}, func(ctx context.Context, log func(string)) error {
		log("one")
		<-release
		log("two")
		return nil
	})
	if err != nil || snap.Status != Running {
		t.Fatalf("Start = %+v, %v", snap, err)
	}
	if _, err := m.Start(Spec{Target: "res"}, func(context.Context, func(string)) error { return nil }); !errors.Is(err, ErrBusy) {
		t.Fatalf("second job on same target: %v", err)
	}
	if running, ok := m.Running("res"); !ok || running.ID != snap.ID {
		t.Fatalf("Running = %+v, %v", running, ok)
	}
	lines, events, _ := m.Watch(context.Background(), snap.ID)
	close(release)
	var streamed []string
	for e := range events {
		if e.Line != "" {
			streamed = append(streamed, e.Line)
		}
		if e.Done != nil && e.Done.Status != Succeeded {
			t.Fatalf("done = %+v", e.Done)
		}
	}
	all := append(lines, streamed...)
	if strings.Join(all, ",") != "one,two" {
		t.Fatalf("log = %v", all)
	}
	// The target is free again.
	if _, err := m.Start(Spec{Target: "res"}, func(context.Context, func(string)) error { return nil }); err != nil {
		t.Fatal(err)
	}
	m.Wait()
}

func TestJobFailureCancelAndPanic(t *testing.T) {
	m := NewManager(context.Background(), 10)
	failed, _ := m.Start(Spec{}, func(context.Context, func(string)) error { return errors.New("boom") })
	if s := waitDone(t, m, failed.ID); s.Status != Failed || s.Error != "boom" {
		t.Fatalf("failed = %+v", s)
	}
	canceled, _ := m.Start(Spec{}, func(ctx context.Context, _ func(string)) error {
		<-ctx.Done()
		return ctx.Err()
	})
	m.Cancel(canceled.ID)
	if s := waitDone(t, m, canceled.ID); s.Status != Canceled {
		t.Fatalf("canceled = %+v", s)
	}
	panicked, _ := m.Start(Spec{}, func(context.Context, func(string)) error { panic("bad") })
	if s := waitDone(t, m, panicked.ID); s.Status != Failed || !strings.Contains(s.Error, "panic: bad") {
		t.Fatalf("panicked = %+v", s)
	}
	// Watching a finished job yields only its final state.
	if s := waitDone(t, m, failed.ID); s.Status != Failed {
		t.Fatalf("rewatch = %+v", s)
	}
}

func TestRetention(t *testing.T) {
	m := NewManager(context.Background(), 2)
	var ids []string
	for range 4 {
		s, _ := m.Start(Spec{}, func(context.Context, func(string)) error { return nil })
		waitDone(t, m, s.ID)
		ids = append(ids, s.ID)
	}
	list := m.List()
	// Pruning happens on Start, so the newest job plus `keep` finished ones remain at most.
	if len(list) > 3 || list[0].ID != ids[3] {
		t.Fatalf("List = %+v", list)
	}
	if _, _, err := m.Get(ids[0]); !errors.Is(err, ErrNotFound) {
		t.Fatalf("oldest job should be pruned: %v", err)
	}
}
