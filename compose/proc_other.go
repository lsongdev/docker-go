//go:build !unix

package compose

import (
	"os/exec"
	"time"
)

func configureCancel(cmd *exec.Cmd) { cmd.WaitDelay = 15 * time.Second }
