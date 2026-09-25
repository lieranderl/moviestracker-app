package packaging_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// macApp builds Moviestracker.app and its DMG with fake TorrServers (small
// programs for both Mac CPUs, since the app joins them into one).
func macApp(t *testing.T) (app, dmg string) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("the Mac app is built on macOS")
	}
	root := repoRoot(t)
	fake := t.TempDir()
	src := filepath.Join(fake, "main.go")
	if err := os.WriteFile(src, []byte("package main\n\nfunc main() { println(\"fake torrserver\") }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, arch := range []string{"arm64", "amd64"} {
		run(t, fake, []string{"GOOS=darwin", "GOARCH=" + arch, "CGO_ENABLED=0", "GO111MODULE=off"},
			"go", "build", "-o", "TorrServer-gst-darwin-"+arch, src)
	}
	if err := os.WriteFile(filepath.Join(fake, "LICENSE"), []byte("GNU GENERAL PUBLIC LICENSE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dist := t.TempDir()
	run(t, root, []string{"TORRSERVER_FROM=" + fake, "DIST=" + dist}, "scripts/macapp.sh", version)
	dmg = filepath.Join(dist, "Moviestracker-"+version+".dmg")

	// Open the DMG like a person would, and take the app from it.
	mount := t.TempDir()
	run(t, dist, nil, "hdiutil", "attach", "-nobrowse", "-readonly", "-mountpoint", mount, dmg)
	t.Cleanup(func() { _ = exec.Command("hdiutil", "detach", "-force", mount).Run() }) // #nosec G204 -- fixed test command
	if link, err := os.Readlink(filepath.Join(mount, "Applications")); err != nil || link != "/Applications" {
		t.Errorf("the DMG has no Applications shortcut to drag the app onto: %q, %v", link, err)
	}
	// Unsigned, so macOS refuses the first open: the DMG says what to do.
	// #nosec G304 -- the test's own mounted DMG
	if help, err := os.ReadFile(filepath.Join(mount, "If macOS won't open it.txt")); err != nil ||
		!strings.Contains(string(help), "Open Anyway") || !strings.Contains(string(help), "Privacy & Security") {
		t.Errorf("the DMG should explain how to open the unsigned app: %q, %v", help, err)
	}
	app = filepath.Join(t.TempDir(), "Moviestracker.app")
	run(t, mount, nil, "cp", "-R", filepath.Join(mount, "Moviestracker.app"), app)
	return app, dmg
}

func TestTheMacAppRunsOnAppleSiliconAndIntel(t *testing.T) {
	app, _ := macApp(t)
	for _, exe := range []string{"Moviestracker", "moviestracker-server", "torrserver"} {
		archs := strings.Fields(run(t, app, nil, "lipo", "-archs", filepath.Join(app, "Contents", "MacOS", exe)))
		if len(archs) != 2 || !strings.Contains(strings.Join(archs, " "), "arm64") || !strings.Contains(strings.Join(archs, " "), "x86_64") {
			t.Errorf("%s is for %v, want arm64 and x86_64", exe, archs)
		}
	}
	if out := run(t, app, nil, filepath.Join(app, "Contents", "MacOS", "moviestracker-server"), "--version"); strings.TrimSpace(out) != "moviestracker "+version {
		t.Errorf("bundled moviestracker --version = %q", out)
	}
	run(t, app, nil, "codesign", "--verify", "--deep", "--strict", app)
}

func TestTheMacAppIsAMenuBarAppWithItsIconAndLicences(t *testing.T) {
	app, _ := macApp(t)
	info := filepath.Join(app, "Contents", "Info.plist")
	for key, want := range map[string]string{
		"CFBundleIdentifier":         "app.moviestracker",
		"CFBundleExecutable":         "Moviestracker",
		"CFBundleShortVersionString": strings.TrimPrefix(version, "v"),
		"CFBundleIconFile":           "AppIcon",
		"LSMinimumSystemVersion":     "13.0",
		"LSUIElement":                "true", // menu bar only, no Dock icon
	} {
		if got := strings.TrimSpace(run(t, app, nil, "plutil", "-extract", key, "raw", info)); got != want {
			t.Errorf("Info.plist %s = %q, want %q", key, got, want)
		}
	}
	for _, f := range []string{
		"Resources/AppIcon.icns", "Resources/MenuIcon.png", "Resources/MenuIcon@2x.png",
		"Resources/LICENSE", "Resources/NOTICE", "Resources/licenses/TorrServer-LICENSE", "Resources/licenses/TorrServer-SOURCE.txt",
	} {
		if _, err := os.Stat(filepath.Join(app, "Contents", f)); err != nil {
			t.Errorf("app lacks %s: %v", f, err)
		}
	}
	// The GStreamer download is the app's own script, pinned to one version.
	script := filepath.Join(app, "Contents", "Resources", "install-gstreamer.sh")
	if got := strings.TrimSpace(run(t, app, nil, script, "--version")); got != "1.28.7" {
		t.Errorf("bundled GStreamer script installs %q, want 1.28.7", got)
	}
}
