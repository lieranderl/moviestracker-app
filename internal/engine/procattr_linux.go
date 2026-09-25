package engine

import "syscall"

// sysProcAttr makes Linux send the engine SIGTERM if Moviestracker dies.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}
