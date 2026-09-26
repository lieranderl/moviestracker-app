//go:build windows

package main

import (
	"errors"
	"os"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/lieranderl/moviestracker-app/internal/tray"
)

// Named objects for this user's session: one tells whether a tray app runs,
// the other asks it to quit.
const (
	instanceName = `Local\Moviestracker.Tray`
	quitName     = `Local\Moviestracker.Quit`
)

// claimInstance makes this the one tray app of the session; running is true
// when another one already is.
func claimInstance() (release func(), running bool) {
	h, err := windows.CreateMutex(nil, false, windows.StringToUTF16Ptr(instanceName))
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		_ = windows.CloseHandle(h)
		return func() {}, true
	}
	if err != nil {
		return func() {}, false
	}
	return func() { _ = windows.CloseHandle(h) }, false
}

// whenAskedToQuit quits when `Moviestracker.exe --quit` asks.
func (a *app) whenAskedToQuit() {
	h, err := windows.CreateEvent(nil, 1, 0, windows.StringToUTF16Ptr(quitName))
	if err != nil {
		tray.Logf(a.dataDir, "cannot listen for --quit: %v", err)
		return
	}
	if _, err := windows.WaitForSingleObject(h, windows.INFINITE); err == nil {
		tray.Logf(a.dataDir, "asked to quit")
		a.quit()
	}
}

// quitRunning asks the running tray app to quit and waits until it has,
// with Moviestracker and TorrServer stopped. It returns the exit code: 0
// when none runs any more.
func quitRunning(timeout time.Duration) int {
	h, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, windows.StringToUTF16Ptr(quitName))
	if err != nil {
		return 0 // none runs
	}
	err = windows.SetEvent(h)
	_ = windows.CloseHandle(h)
	if err != nil {
		return 1
	}
	for deadline := time.Now().Add(timeout); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		m, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(instanceName))
		if err != nil {
			return 0
		}
		_ = windows.CloseHandle(m)
	}
	return 1
}

// shellOpen opens a web address in the browser, or a file in its app.
func shellOpen(target string) {
	_ = windows.ShellExecute(0, windows.StringToUTF16Ptr("open"), windows.StringToUTF16Ptr(target), nil, nil, windows.SW_SHOWNORMAL)
}

func message(title, text string) {
	_, _ = windows.MessageBox(0, windows.StringToUTF16Ptr(text), windows.StringToUTF16Ptr(title), windows.MB_OK|windows.MB_ICONINFORMATION)
}

func fatal(text string) {
	_, _ = windows.MessageBox(0, windows.StringToUTF16Ptr(text), windows.StringToUTF16Ptr("Moviestracker"), windows.MB_OK|windows.MB_ICONERROR)
	os.Exit(1)
}

// Start at sign-in is a value under the user's Run key, which the installer
// sets when "Start Moviestracker when I sign in" is ticked.
const (
	runKey   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue = "Moviestracker"
)

func startsAtSignIn(exe string) bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer func() { _ = k.Close() }()
	v, _, err := k.GetStringValue(runValue)
	return err == nil && strings.EqualFold(strings.Trim(v, `"`), exe)
}

func setStartAtSignIn(exe string, on bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer func() { _ = k.Close() }()
	if !on {
		return k.DeleteValue(runValue)
	}
	return k.SetStringValue(runValue, `"`+exe+`"`)
}

var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	kernel32         = windows.NewLazySystemDLL("kernel32.dll")
	openClipboard    = user32.NewProc("OpenClipboard")
	emptyClipboard   = user32.NewProc("EmptyClipboard")
	setClipboardData = user32.NewProc("SetClipboardData")
	closeClipboard   = user32.NewProc("CloseClipboard")
	globalAlloc      = kernel32.NewProc("GlobalAlloc")
	globalLock       = kernel32.NewProc("GlobalLock")
	globalUnlock     = kernel32.NewProc("GlobalUnlock")
	globalFree       = kernel32.NewProc("GlobalFree")
	moveMemory       = kernel32.NewProc("RtlMoveMemory")
)

// copyText puts text on the clipboard.
func copyText(text string) error {
	const cfUnicodeText, gmemMoveable = 13, 0x0002
	utf16, err := windows.UTF16FromString(text)
	if err != nil {
		return err
	}
	if r, _, err := openClipboard.Call(0); r == 0 {
		return err
	}
	defer func() { _, _, _ = closeClipboard.Call() }()
	if r, _, err := emptyClipboard.Call(); r == 0 {
		return err
	}
	size := uintptr(len(utf16)) * unsafe.Sizeof(utf16[0])
	mem, _, err := globalAlloc.Call(gmemMoveable, size)
	if mem == 0 {
		return err
	}
	ptr, _, err := globalLock.Call(mem)
	if ptr == 0 {
		_, _, _ = globalFree.Call(mem)
		return err
	}
	_, _, _ = moveMemory.Call(ptr, uintptr(unsafe.Pointer(&utf16[0])), size) // #nosec G103 -- copies the text into the clipboard's memory
	_, _, _ = globalUnlock.Call(mem)
	if r, _, err := setClipboardData.Call(cfUnicodeText, mem); r == 0 {
		_, _, _ = globalFree.Call(mem)
		return err
	}
	return nil // the clipboard owns mem now
}
