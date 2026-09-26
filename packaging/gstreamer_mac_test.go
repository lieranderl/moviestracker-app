// The Linux archive and the Mac app; scripts/ci/windows-install-e2e.ps1
// checks the Windows installer.

//go:build unix

package packaging_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeGStreamerPkg builds a small installer shaped like GStreamer's macOS
// runtime: one package per part, each with a Payload for the framework.
func fakeGStreamerPkg(t *testing.T) (pkg, sum string) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("the GStreamer download is for the Mac app")
	}
	work := t.TempDir()
	parts := map[string][]string{
		"base-system-1.0":                     {"lib/libglib-2.0.0.dylib"},
		"base-crypto":                         {"lib/libcrypto.3.dylib"},
		"gstreamer-1.0-core":                  {"lib/libgstreamer-1.0.0.dylib", "lib/libgstapp-1.0.0.dylib", "libexec/gstreamer-1.0/gst-plugin-scanner", "bin/gst-discoverer-1.0"},
		"gstreamer-1.0-playback":              {"lib/gstreamer-1.0/libgstplayback.dylib"},
		"gstreamer-1.0-codecs":                {"lib/gstreamer-1.0/libgstmatroska.dylib"},
		"gstreamer-1.0-codecs-restricted":     {"lib/gstreamer-1.0/libgstdtsdec.dylib"},
		"gstreamer-1.0-codecs-gpl-restricted": {"lib/gstreamer-1.0/libgstx264.dylib"},
		"gstreamer-1.0-libav":                 {"lib/gstreamer-1.0/libgstlibav.dylib"},
		"gstreamer-1.0-system":                {"lib/gstreamer-1.0/libgstapplemedia.dylib"},
		"gstreamer-1.0-net":                   {"lib/libsoup-3.0.0.dylib", "lib/gstreamer-1.0/libgstsoup.dylib", "lib/gstreamer-1.0/libgstwebrtc.dylib"},
		"gstreamer-1.0-devtools":              {"lib/gstreamer-1.0/libgstvalidate.dylib"},
	}
	var args []string
	for part, files := range parts {
		root := filepath.Join(work, part)
		for _, f := range files {
			p := filepath.Join(root, f)
			if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte(part+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		comp := filepath.Join(work, part+"-1.28.7-universal.pkg")
		run(t, work, nil, "pkgbuild", "--quiet", "--root", root, "--identifier", "org.freedesktop.gstreamer.test."+part,
			"--version", "1.28.7", "--install-location", "/Library/Frameworks/GStreamer.framework/Versions/1.0", comp)
		args = append(args, "--package", comp)
	}
	pkg = filepath.Join(work, "gstreamer-1.0-1.28.7-universal.pkg")
	run(t, work, nil, "productbuild", append(append([]string{"--quiet"}, args...), pkg)...)
	data, err := os.ReadFile(pkg) // #nosec G304 -- the test's own fixture
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(data)
	return pkg, hex.EncodeToString(h[:])
}

func installGStreamer(root, pkg, sum string) ([]byte, error) {
	script := filepath.Join("..", "macos", "install-gstreamer.sh")
	cmd := exec.Command(script, root) // #nosec G204 -- the script under test
	cmd.Env = append(os.Environ(), "MT_GST_URL=file://"+pkg, "MT_GST_SHA256="+sum)
	return cmd.CombinedOutput()
}

func TestTheMacAppDownloadsOnlyTheGStreamerPartsTorrServerUses(t *testing.T) {
	pkg, sum := fakeGStreamerPkg(t)
	root := filepath.Join(t.TempDir(), "gstreamer")
	old := filepath.Join(root, "1.26.0", "lib")
	if err := os.MkdirAll(old, 0o750); err != nil {
		t.Fatal(err)
	}
	if out, err := installGStreamer(root, pkg, sum); err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	dir := filepath.Join(root, "1.28.7")
	for _, want := range []string{
		"lib/libgstreamer-1.0.0.dylib", "lib/libglib-2.0.0.dylib", "libexec/gstreamer-1.0/gst-plugin-scanner",
		"lib/gstreamer-1.0/libgstmatroska.dylib", "lib/gstreamer-1.0/libgstx264.dylib", "lib/gstreamer-1.0/libgstlibav.dylib",
		"lib/gstreamer-1.0/libgstapplemedia.dylib", "lib/libsoup-3.0.0.dylib", "lib/gstreamer-1.0/libgstsoup.dylib",
	} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("missing %s", want)
		}
	}
	for _, unwanted := range []string{"lib/gstreamer-1.0/libgstwebrtc.dylib", "lib/gstreamer-1.0/libgstvalidate.dylib"} {
		if _, err := os.Stat(filepath.Join(dir, unwanted)); err == nil {
			t.Errorf("%s is not needed by TorrServer", unwanted)
		}
	}
	entries, _ := os.ReadDir(root)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "1.28.7" {
		t.Errorf("GStreamer folder holds %v, want only the new version (no older one, download or leftovers)", names)
	}
}

func TestAGStreamerDownloadWithTheWrongChecksumInstallsNothing(t *testing.T) {
	pkg, _ := fakeGStreamerPkg(t)
	root := filepath.Join(t.TempDir(), "gstreamer")
	if out, err := installGStreamer(root, pkg, strings.Repeat("0", 64)); err == nil {
		t.Fatalf("install with a wrong checksum succeeded:\n%s", out)
	} else if !strings.Contains(string(out), "checksum") {
		t.Errorf("the failure should name the checksum: %s", out)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Errorf("a failed install left %d entries behind", len(entries))
	}
}

func TestTheAppKnowsThePinnedGStreamerAndItsDownloadSize(t *testing.T) {
	script := filepath.Join("macos", "install-gstreamer.sh")
	if out := run(t, "..", nil, script, "--size"); strings.TrimSpace(out) != "153594157" {
		t.Errorf("--size = %q, want the pinned installer's size, for progress", out)
	}
	if out := run(t, "..", nil, script, "--version"); strings.TrimSpace(out) != "1.28.7" {
		t.Errorf("--version = %q, want the pinned GStreamer", out)
	}
}
