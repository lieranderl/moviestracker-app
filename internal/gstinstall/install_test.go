// The installer runs the macOS app's shell script; Windows has none.

//go:build unix

package gstinstall_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/gstinstall"
)

// fakeScript stands in for install-gstreamer.sh: it reports version 1.28.7
// and a 1000-byte download, writes half the download, waits for the test to
// let it go on, then installs <root>/1.28.7 (or fails when told to).
func fakeScript(t *testing.T) (script, gate string) {
	t.Helper()
	dir := t.TempDir()
	gate = filepath.Join(dir, "go")
	script = filepath.Join(dir, "install-gstreamer.sh")
	body := `#!/bin/sh
case "$1" in
  --version) echo 1.28.7; exit 0 ;;
  --size) echo 1000; exit 0 ;;
esac
root="$1"
mkdir -p "$root/.download"
head -c 500 /dev/zero > "$root/.download/gstreamer.pkg"
while [ ! -f "` + gate + `" ]; do sleep 0.05; done
if grep -q fail "` + gate + `"; then
  echo "install-gstreamer: the download's checksum is abc, not def" >&2
  rm -rf "$root/.download"
  exit 1
fi
rm -rf "$root/.download"
mkdir -p "$root/1.28.7/lib"
touch "$root/1.28.7/lib/libgstreamer-1.0.0.dylib"
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil { // #nosec G306 -- an executable fixture
		t.Fatal(err)
	}
	return script, gate
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestInstallingGStreamerShowsProgressThenUsesIt(t *testing.T) {
	script, gate := fakeScript(t)
	root := filepath.Join(t.TempDir(), "gstreamer")
	var mu sync.Mutex
	var usedDir string
	inst := gstinstall.New(script, root, func(_ context.Context, dir string) error {
		mu.Lock()
		defer mu.Unlock()
		usedDir = dir
		return nil
	})
	defer inst.Close()

	st := inst.Status()
	if st.Phase != gstinstall.Idle || st.Pinned != "1.28.7" || st.Total != 1000 || st.Installed != "" {
		t.Fatalf("before install = %+v", st)
	}
	if !inst.Start() {
		t.Fatal("Start() = false, want the install to begin")
	}
	if inst.Start() {
		t.Error("a second Start() while installing should not start another")
	}
	waitFor(t, "download progress", func() bool {
		st := inst.Status()
		return st.Phase == gstinstall.Downloading && st.Downloaded == 500
	})
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	// Once the download is gone it is unpacking, not starting over.
	waitFor(t, "the install to finish or unpack", func() bool {
		st := inst.Status()
		if st.Phase == gstinstall.Downloading && st.Downloaded == 0 {
			t.Fatalf("after the download went away status = %+v, want installing", st)
		}
		return st.Phase == gstinstall.Done
	})
	waitFor(t, "the install to finish", func() bool { return inst.Status().Phase == gstinstall.Done })
	if st := inst.Status(); st.Installed != "1.28.7" || st.Error != "" {
		t.Errorf("after install = %+v", st)
	}
	mu.Lock()
	defer mu.Unlock()
	if usedDir != filepath.Join(root, "1.28.7") {
		t.Errorf("TorrServer pointed at %q, want the new folder", usedDir)
	}
}

func TestAFailedGStreamerInstallSaysWhyAndCanBeRetried(t *testing.T) {
	script, gate := fakeScript(t)
	root := filepath.Join(t.TempDir(), "gstreamer")
	inst := gstinstall.New(script, root, func(context.Context, string) error {
		t.Error("a failed install must not be used")
		return nil
	})
	defer inst.Close()
	if err := os.WriteFile(gate, []byte("fail"), 0o600); err != nil {
		t.Fatal(err)
	}
	inst.Start()
	waitFor(t, "the failure", func() bool { return inst.Status().Phase == gstinstall.Failed })
	if st := inst.Status(); !strings.Contains(st.Error, "checksum") || strings.Contains(st.Error, "install-gstreamer:") {
		t.Errorf("error = %q, want the script's reason without its prefix", st.Error)
	}
	if !inst.Start() {
		t.Error("after a failure the install can be tried again")
	}
}

func TestAnEarlierInstallIsFoundAndAnOlderOneNeedsAnUpdate(t *testing.T) {
	script, _ := fakeScript(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "1.26.0", "lib"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "1.26.0", "lib", "libgstreamer-1.0.0.dylib"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	inst := gstinstall.New(script, root, nil)
	defer inst.Close()
	st := inst.Status()
	if st.Installed != "1.26.0" || !st.UpdateAvailable() {
		t.Errorf("status = %+v; want 1.26.0 installed and 1.28.7 offered", st)
	}
	if dir, ok := gstinstall.InstalledIn(root); !ok || dir != filepath.Join(root, "1.26.0") {
		t.Errorf("InstalledIn() = %q, %v", dir, ok)
	}
	if _, ok := gstinstall.InstalledIn(filepath.Join(root, "missing")); ok {
		t.Error("an empty folder holds no GStreamer")
	}
}

func TestTheInstallIsReadyOnlyOnceTorrServerRunsWithGStreamer(t *testing.T) {
	script, gate := fakeScript(t)
	var ready atomic.Bool
	inst := gstinstall.New(script, filepath.Join(t.TempDir(), "gstreamer"), nil,
		gstinstall.WithCheck(func(context.Context) bool { return ready.Load() }, 10*time.Millisecond, 10*time.Second))
	defer inst.Close()
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	inst.Start()
	waitFor(t, "TorrServer to start", func() bool { return inst.Status().Phase == gstinstall.Starting })
	time.Sleep(50 * time.Millisecond)
	if st := inst.Status(); st.Phase != gstinstall.Starting {
		t.Fatalf("before TorrServer uses GStreamer status = %+v, want still starting", st)
	}
	ready.Store(true)
	waitFor(t, "ready", func() bool { return inst.Status().Phase == gstinstall.Done })
}

func TestATorrServerThatDoesNotLoadGStreamerFailsTheInstall(t *testing.T) {
	script, gate := fakeScript(t)
	inst := gstinstall.New(script, filepath.Join(t.TempDir(), "gstreamer"), nil,
		gstinstall.WithCheck(func(context.Context) bool { return false }, 10*time.Millisecond, 200*time.Millisecond))
	defer inst.Close()
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	inst.Start()
	waitFor(t, "the failure", func() bool { return inst.Status().Phase == gstinstall.Failed })
	if st := inst.Status(); !strings.Contains(st.Error, "TorrServer") {
		t.Errorf("error = %q, want it to say TorrServer did not start with GStreamer", st.Error)
	}
}

// Once GStreamer is ready its message may be closed; an install under way
// keeps showing its progress.
func TestAFinishedInstallCanBeDismissedButARunningOneCannot(t *testing.T) {
	script, gate := fakeScript(t)
	inst := gstinstall.New(script, filepath.Join(t.TempDir(), "gstreamer"), func(context.Context, string) error { return nil })
	defer inst.Close()

	inst.Start()
	waitFor(t, "the download", func() bool { return inst.Status().Phase == gstinstall.Downloading })
	inst.Dismiss()
	if st := inst.Status(); st.Phase != gstinstall.Downloading {
		t.Fatalf("dismissing a running install left it %q", st.Phase)
	}

	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the install to finish", func() bool { return inst.Status().Phase == gstinstall.Done })
	inst.Dismiss()
	if st := inst.Status(); st.Phase != gstinstall.Idle || st.Installed != "1.28.7" {
		t.Errorf("after dismissing a finished install: %+v, want idle with 1.28.7 installed", st)
	}
}
