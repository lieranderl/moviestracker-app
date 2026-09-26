//go:build !linux && !windows

package engine

import (
	"os"
	"syscall"
)

// sysProcAttr is empty where there is no parent-death signal (macOS); an
// engine left by a crash is replaced on the next start instead.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

// tieToMoviestracker does nothing here: macOS has no way to end a child with
// its parent.
func tieToMoviestracker(*os.Process) {}
