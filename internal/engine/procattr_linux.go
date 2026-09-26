package engine

import (
	"os"
	"syscall"
)

// sysProcAttr makes Linux send the engine SIGTERM if Moviestracker dies.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}

// tieToMoviestracker does nothing more on Linux: Pdeathsig does it.
func tieToMoviestracker(*os.Process) {}
