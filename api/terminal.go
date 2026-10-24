package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/coder/websocket"
	"github.com/lsongdev/docker-go/engine"
)

// autoShell prefers bash and falls back to sh.
var autoShell = []string{"/bin/sh", "-c", "if command -v bash >/dev/null 2>&1; then exec bash; else exec sh; fi"}

// terminalControl is a text frame sent by the client.
type terminalControl struct {
	Type string `json:"type"` // "resize"
	Cols uint   `json:"cols"`
	Rows uint   `json:"rows"`
}

// containerTerminal opens an interactive shell in a running container over
// WebSocket. Binary frames carry raw terminal bytes in both directions; the
// client sends text frames with JSON control messages such as
// {"type":"resize","cols":120,"rows":40}. The socket closes with the shell's
// exit code as the reason.
func (s *Server) containerTerminal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	q := r.URL.Query()
	cols, rows := dimension(q.Get("cols"), 80), dimension(q.Get("rows"), 24)
	cmd := autoShell
	if shell := q.Get("shell"); shell != "" {
		cmd = []string{shell}
	}
	// Report problems as JSON while we still can, before upgrading.
	c, err := s.Engine.InspectContainer(r.Context(), id)
	if err != nil {
		fail(w, err)
		return
	}
	if c.State == nil || !c.State.Running {
		writeError(w, http.StatusConflict, "conflict", "container is not running")
		return
	}
	conn, err := websocket.Accept(w, r, nil) // rejects cross-origin requests
	if err != nil {
		return
	}
	defer conn.CloseNow()

	ctx := r.Context()
	size := [2]uint{rows, cols}
	execID, err := s.Engine.CreateExec(ctx, id, engine.ExecConfig{
		Cmd: cmd, Env: []string{"TERM=xterm-256color"}, Tty: true,
		AttachStdin: true, AttachStdout: true, AttachStderr: true, ConsoleSize: &size,
	})
	if err != nil {
		closeWith(conn, websocket.StatusInternalError, err.Error())
		return
	}
	stream, err := s.Engine.AttachExec(ctx, execID, true, &size)
	if err != nil {
		closeWith(conn, websocket.StatusInternalError, err.Error())
		return
	}
	defer stream.Close()

	// Each direction only tears down its own side: cancelling a pending
	// conn.Read would drop the socket before the close frame is sent.
	//
	// Container output → browser; ends when the shell exits.
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 32<<10)
		for {
			n, err := stream.Read(buf)
			if n > 0 && conn.Write(ctx, websocket.MessageBinary, buf[:n]) != nil {
				return
			}
			if err != nil {
				return
			}
		}
	}()

	// Browser input → container; when the browser goes away, closing the
	// exec stream ends the output loop above.
	go func() {
		defer stream.Close()
		for {
			kind, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if kind == websocket.MessageBinary {
				if _, err := stream.Write(data); err != nil {
					return
				}
				continue
			}
			var ctl terminalControl
			if json.Unmarshal(data, &ctl) == nil && ctl.Type == "resize" && ctl.Cols > 0 && ctl.Rows > 0 {
				s.Engine.ResizeExec(ctx, execID, ctl.Rows, ctl.Cols)
			}
		}
	}()

	<-done
	reason := "session closed"
	if info, err := s.Engine.InspectExec(context.WithoutCancel(ctx), execID); err == nil && !info.Running {
		reason = fmt.Sprintf("exited with code %d", info.ExitCode)
	}
	closeWith(conn, websocket.StatusNormalClosure, reason)
}

func dimension(v string, fallback uint) uint {
	n, err := strconv.ParseUint(v, 10, 16)
	if err != nil || n == 0 {
		return fallback
	}
	return uint(n)
}

// closeWith closes the socket; close reasons are limited to 123 bytes.
func closeWith(conn *websocket.Conn, code websocket.StatusCode, reason string) {
	if len(reason) > 120 {
		reason = reason[:120]
	}
	conn.Close(code, reason)
}
