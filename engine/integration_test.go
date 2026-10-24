//go:build integration

// Run against a real daemon with: go test -tags integration ./engine
package engine_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/lsongdev/docker-go/engine"
)

func TestDaemonContainerLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	c, err := engine.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	const image = "alpine:3.20"
	if err := c.PullImage(ctx, image, engine.PullOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	name := "docker-go-it-" + time.Now().Format("150405")
	id, err := c.CreateContainer(ctx, name, engine.ContainerCreate{
		ContainerConfig: engine.ContainerConfig{Image: image, Cmd: []string{"sh", "-c", "echo out; echo err >&2; sleep 300"}, Labels: map[string]string{"docker-go.test": "1"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.RemoveContainer(context.Background(), id, engine.RemoveContainerOptions{Force: true})
	if err := c.StartContainer(ctx, id); err != nil {
		t.Fatal(err)
	}

	time.Sleep(500 * time.Millisecond)
	var lines []string
	if err := c.Logs(ctx, id, engine.LogsOptions{Tail: "all"}, func(l engine.LogLine) error {
		lines = append(lines, string(l.Stream)+":"+l.Text)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(lines, ","); !strings.Contains(got, "stdout:out") || !strings.Contains(got, "stderr:err") {
		t.Fatalf("logs = %s", got)
	}

	execID, err := c.CreateExec(ctx, id, engine.ExecConfig{Cmd: []string{"cat"}, Tty: true, AttachStdin: true, AttachStdout: true})
	if err != nil {
		t.Fatal(err)
	}
	conn, err := c.AttachExec(ctx, execID, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	conn.Write([]byte("ping\n\x04"))
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	out, _ := io.ReadAll(conn)
	conn.Close()
	if !strings.Contains(string(out), "ping") {
		t.Fatalf("exec output = %q", out)
	}

	list, err := c.ListContainers(ctx, engine.ListContainersOptions{Filters: engine.Filters{"label": {"docker-go.test=1"}}})
	if err != nil || len(list) != 1 || list[0].Name() != name {
		t.Fatalf("filtered list = %+v, %v", list, err)
	}
	timeout := time.Second
	if err := c.StopContainer(ctx, id, &timeout); err != nil {
		t.Fatal(err)
	}
	info, err := c.InspectContainer(ctx, id)
	if err != nil || info.State.Running {
		t.Fatalf("after stop: %+v, %v", info.State, err)
	}
}
