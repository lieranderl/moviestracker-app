package engine_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/engine"
)

func TestTheTorrServerProgramIsFoundWhereAnInstallPutsIt(t *testing.T) {
	appDir, dataDir, custom := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "ts")
	executable := filepath.Join(appDir, "moviestracker")
	place := func(path string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil { // #nosec G306 -- an executable fixture
			t.Fatal(err)
		}
	}
	env := map[string]string{}
	find := func() string { return engine.FindBinary(func(k string) string { return env[k] }, executable, dataDir) }

	if got := find(); got != "" {
		t.Fatalf("FindBinary() with nothing installed = %q, want empty", got)
	}
	inData := filepath.Join(dataDir, "engine", "bin", "torrserver")
	place(inData)
	if got := find(); got != inData {
		t.Errorf("FindBinary() = %q, want the data directory copy %q", got, inData)
	}
	beside := filepath.Join(appDir, "torrserver")
	place(beside)
	if got := find(); got != beside {
		t.Errorf("FindBinary() = %q, want the program next to moviestracker %q", got, beside)
	}
	place(custom)
	env["MT_TORRSERVER_BIN"] = custom
	if got := find(); got != custom {
		t.Errorf("FindBinary() = %q, want MT_TORRSERVER_BIN %q", got, custom)
	}
	env["MT_TORRSERVER_BIN"] = filepath.Join(appDir, "missing")
	if got := find(); got != "" {
		t.Errorf("FindBinary() with a wrong MT_TORRSERVER_BIN = %q, want empty (no silent fallback)", got)
	}
}
