package engine

import (
	"log/slog"
	"os"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// sysProcAttr runs the engine without a console window of its own.
func sysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
}

// job holds every engine this Moviestracker starts. Windows closes the job
// when Moviestracker exits, however it exits, and that ends the engines.
var job = sync.OnceValue(func() windows.Handle {
	h, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		slog.Warn("cannot tie TorrServer to Moviestracker", "error", err)
		return 0
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE},
	}
	if _, err := windows.SetInformationJobObject(h, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil { // #nosec G103 -- the Win32 API takes a pointer
		slog.Warn("cannot tie TorrServer to Moviestracker", "error", err)
		_ = windows.CloseHandle(h)
		return 0
	}
	return h
})

// tieToMoviestracker puts the engine in the job, so it ends with Moviestracker.
func tieToMoviestracker(p *os.Process) {
	j := job()
	if j == 0 {
		return
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(p.Pid)) // #nosec G115 -- a PID
	if err != nil {
		slog.Warn("cannot tie TorrServer to Moviestracker", "error", err)
		return
	}
	defer func() { _ = windows.CloseHandle(h) }()
	if err := windows.AssignProcessToJobObject(j, h); err != nil {
		slog.Warn("cannot tie TorrServer to Moviestracker", "error", err)
	}
}
