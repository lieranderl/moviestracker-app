//go:build !windows

package tray

import "syscall"

// sysProcAttr has nothing to add outside Windows, where the tests run too.
func sysProcAttr() *syscall.SysProcAttr { return nil }
