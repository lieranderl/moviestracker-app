//go:build !linux

package engine

import "syscall"

// sysProcAttr is empty where there is no parent-death signal (macOS); an
// engine left by a crash is replaced on the next start instead.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}
