//go:build unix

package gstinstall

import (
	"os/exec"
	"syscall"
)

// stopsWithItsChildren runs cmd in its own process group, so stopping it
// stops its curl too.
func stopsWithItsChildren(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
}
