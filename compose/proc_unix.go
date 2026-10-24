//go:build unix

package compose

import (
	"os/exec"
	"syscall"
	"time"
)

// configureCancel runs the command in its own process group and, on context
// cancellation, interrupts the whole group: `docker compose` delegates to a
// plugin process that would otherwise be orphaned. SIGTERM lets compose stop
// what it started; WaitDelay forces a kill if it does not exit.
func configureCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	}
	cmd.WaitDelay = 15 * time.Second
}
