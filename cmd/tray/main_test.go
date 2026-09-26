//go:build windows

package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/tray"
)

// Without a taskbar (as on CI runners) the tray library only reports that
// it is not ready, so building the menu runs through all of the app's own
// steps.
func TestTheTrayIconGetsItsMenu(t *testing.T) {
	dir := t.TempDir()
	a := &app{dataDir: dir, exe: filepath.Join(dir, "Moviestracker.exe")}
	a.server = tray.NewServer(tray.Config{Program: filepath.Join(dir, "moviestracker-server.exe"), DataDir: dir, Port: port})

	built := make(chan struct{})
	go func() {
		a.ready()
		close(built)
	}()
	select {
	case <-built:
	case <-time.After(5 * time.Second):
		t.Fatal("the tray never finished its menu: clicking its icon would show nothing")
	}
}
