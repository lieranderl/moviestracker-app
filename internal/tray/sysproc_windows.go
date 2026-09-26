package tray

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// sysProcAttr runs the server without a console window.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}
