// The Linux archive and the Mac app; scripts/ci/windows-install-e2e.ps1
// checks the Windows installer.

//go:build unix

// Package packaging_test checks the release, install and Mac app scripts by
// running them: a Linux archive installed into a temporary prefix on a
// sandboxed "Linux" PATH, and the Mac app and its DMG. Fake TorrServer
// binaries keep it offline and fast.
package packaging_test

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const version = "v0.0.0-test"

// testSharedTMDBKey stands in for the shared TMDB key release builds carry.
const testSharedTMDBKey = "test-shared-tmdb-key" // #nosec G101 -- a test fixture

func run(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...) // #nosec G204 -- the tests' own fixed commands
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}

// release builds the Linux archive for this CPU with a fake TorrServer.
func release(t *testing.T) (archive string, dist string) {
	t.Helper()
	return releaseFor(t, "linux", runtime.GOARCH)
}

// releaseFor builds the archive for goos/goarch with a fake TorrServer.
func releaseFor(t *testing.T, goos, goarch string) (archive string, dist string) {
	t.Helper()
	root := repoRoot(t)
	fake := t.TempDir()
	target := goos + "-" + goarch
	if err := os.WriteFile(filepath.Join(fake, "TorrServer-gst-"+target), []byte("#!/bin/sh\necho fake torrserver\n"), 0o755); err != nil { // #nosec G306 -- an executable fixture
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fake, "LICENSE"), []byte("GNU GENERAL PUBLIC LICENSE\n"), 0o644); err != nil { // #nosec G306 -- a fixture
		t.Fatal(err)
	}
	dist = t.TempDir()
	run(t, root, []string{
		"TARGETS=" + goos + "/" + goarch,
		"TORRSERVER_FROM=" + fake, // skip the download and checksum of the real release
		"DIST=" + dist,
		"MACAPP=no", // the Mac app has its own tests
		"MT_SHARED_TMDB_KEY=" + testSharedTMDBKey,
	}, "scripts/release.sh", version)
	return filepath.Join(dist, "moviestracker_"+version+"_"+goos+"_"+goarch+".tar.gz"), dist
}

func untar(t *testing.T, archive, into string) map[string]os.FileMode {
	t.Helper()
	f, err := os.Open(archive) // #nosec G304 -- the archive the test built
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	modes := map[string]os.FileMode{}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return modes
		}
		if err != nil {
			t.Fatal(err)
		}
		modes[h.Name] = h.FileInfo().Mode()
		if h.Typeflag != tar.TypeReg {
			continue
		}
		dst := filepath.Join(into, filepath.Clean(h.Name)) // #nosec G305 -- entries of the archive the test built
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			t.Fatal(err)
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY, h.FileInfo().Mode()) // #nosec G304 -- see above
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.Copy(out, tr); err != nil { // #nosec G110 -- small archive the test built
			t.Fatal(err)
		}
		_ = out.Close()
	}
}

func TestAReleaseArchiveHoldsEverythingToInstall(t *testing.T) {
	archive, dist := release(t)
	dir := strings.TrimSuffix(filepath.Base(archive), ".tar.gz") + "/"
	modes := untar(t, archive, t.TempDir())
	for name, exec := range map[string]bool{
		"moviestracker": true, "torrserver": true, "install.sh": true,
		"LICENSE": false, "NOTICE": false, "licenses/TorrServer-LICENSE": false, "licenses/TorrServer-SOURCE.txt": false, "README.txt": false,
	} {
		mode, ok := modes[dir+name]
		if !ok {
			t.Errorf("archive lacks %s", name)
			continue
		}
		if exec && mode.Perm()&0o111 == 0 {
			t.Errorf("%s is not executable (%v)", name, mode)
		}
	}

	sums, err := os.ReadFile(filepath.Join(dist, "checksums.txt")) // #nosec G304 -- the test's own output
	if err != nil {
		t.Fatalf("no checksums.txt: %v", err)
	}
	raw, _ := os.ReadFile(archive) // #nosec G304 -- see above
	sum := sha256.Sum256(raw)
	if want := hex.EncodeToString(sum[:]) + "  " + filepath.Base(archive); !strings.Contains(string(sums), want) {
		t.Errorf("checksums.txt = %q, want a line %q", sums, want)
	}
}

func TestTheBinaryInAReleaseKnowsItsVersion(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the archive's program runs on Linux; the Mac app's test checks its own")
	}
	archive, _ := release(t)
	into := t.TempDir()
	untar(t, archive, into)
	dir := filepath.Join(into, strings.TrimSuffix(filepath.Base(archive), ".tar.gz"))
	if out := run(t, dir, nil, "./moviestracker", "--version"); strings.TrimSpace(out) != "moviestracker "+version {
		t.Errorf("--version = %q", out)
	}
}

// installed installs the archive into a temporary prefix on a sandboxed
// Linux machine, without touching the service manager.
func installed(t *testing.T) (prefix, unpacked string, m machine) {
	t.Helper()
	archive, _ := release(t)
	into := t.TempDir()
	untar(t, archive, into)
	unpacked = filepath.Join(into, strings.TrimSuffix(filepath.Base(archive), ".tar.gz"))
	prefix = t.TempDir()
	m = newMachine(t, linux, "")
	run(t, unpacked, m.env(), "./install.sh", "--prefix", prefix, "--no-service", "--without-gstreamer")
	return prefix, unpacked, m
}

func layout(prefix string) (bin, engine, env, data, service, uninstall string) {
	lib := filepath.Join(prefix, "usr", "local", "lib", "moviestracker")
	return filepath.Join(prefix, "usr", "local", "bin", "moviestracker"),
		filepath.Join(lib, "torrserver"),
		filepath.Join(prefix, "etc", "moviestracker", "moviestracker.env"),
		filepath.Join(prefix, "var", "lib", "moviestracker"),
		filepath.Join(prefix, "etc", "systemd", "system", "moviestracker.service"),
		filepath.Join(lib, "uninstall.sh")
}

func TestInstallPutsTheProgramsServiceAndDataInPlace(t *testing.T) {
	prefix, _, _ := installed(t)
	bin, engine, env, data, service, uninstall := layout(prefix)
	for _, p := range []string{bin, engine, uninstall} {
		if info, err := os.Stat(p); err != nil || info.Mode().Perm()&0o111 == 0 {
			t.Errorf("%s: %v, want an executable", p, err)
		}
	}
	if info, err := os.Stat(data); err != nil || !info.IsDir() || info.Mode().Perm()&0o007 != 0 {
		t.Errorf("data dir %s: %v; want a folder others cannot read", data, err)
	}
	unit, err := os.ReadFile(service) // #nosec G304 -- the test's own prefix
	if err != nil {
		t.Fatalf("service definition: %v", err)
	}
	for _, want := range []string{"User=moviestracker", "EnvironmentFile=/etc/moviestracker/moviestracker.env", "ExecStart=/usr/local/bin/moviestracker",
		"NoNewPrivileges=true", "ProtectSystem=strict", "ReadWritePaths=/var/lib/moviestracker", "KillMode=mixed", "Restart=on-failure"} {
		if !strings.Contains(string(unit), want) {
			t.Errorf("service definition lacks %q:\n%s", want, unit)
		}
	}
	conf, err := os.ReadFile(env) // #nosec G304 -- see above
	if err != nil || !strings.Contains(string(conf), "MT_LISTEN=:8095") || !strings.Contains(string(conf), "MT_TORRSERVER_BIN=/usr/local/lib/moviestracker/torrserver") {
		t.Errorf("env file: %v\n%s", err, conf)
	}
}

func TestReinstallingUpgradesAndKeepsTheSettings(t *testing.T) {
	prefix, unpacked, m := installed(t)
	_, _, env, data, _, _ := layout(prefix)
	state := filepath.Join(data, "moviestracker.json")
	if err := os.WriteFile(state, []byte(`{"users":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(env, []byte("MT_LISTEN=:9000\n"), 0o640); err != nil { // #nosec G306 -- mirrors the installer
		t.Fatal(err)
	}
	run(t, unpacked, m.env(), "./install.sh", "--prefix", prefix, "--no-service", "--without-gstreamer")
	if raw, err := os.ReadFile(state); err != nil || string(raw) != `{"users":[]}` { // #nosec G304 -- test prefix
		t.Errorf("state after reinstall = %q, %v; want it untouched", raw, err)
	}
	if raw, _ := os.ReadFile(env); string(raw) != "MT_LISTEN=:9000\n" { // #nosec G304 -- test prefix
		t.Errorf("env file after reinstall = %q, want the admin's edit kept", raw)
	}
}

func TestTheInstalledUninstallerRemovesItAndTorrServerAndKeepsSettingsUnlessPurged(t *testing.T) {
	prefix, unpacked, m := installed(t)
	bin, _, _, data, service, uninstall := layout(prefix)
	state := filepath.Join(data, "moviestracker.json")
	if err := os.WriteFile(state, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}
	torrents := filepath.Join(data, "engine", "config.db") // TorrServer's torrent list and settings
	if err := os.MkdirAll(filepath.Dir(torrents), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(torrents, []byte("db"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The archive may be long gone: the copy installed with it is enough.
	run(t, t.TempDir(), m.env(), uninstall, "--prefix", prefix, "--no-service")
	for _, gone := range []string{bin, service, uninstall, filepath.Dir(torrents)} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("%s still there after uninstall", gone)
		}
	}
	if _, err := os.Stat(state); err != nil {
		t.Errorf("uninstall removed the data: %v", err)
	}
	run(t, unpacked, m.env(), "./install.sh", "--prefix", prefix, "--no-service", "--uninstall", "--purge")
	if _, err := os.Stat(data); !os.IsNotExist(err) {
		t.Errorf("purge left the data folder: %v", err)
	}
}

func TestOnAMacTheInstallerPointsToTheApp(t *testing.T) {
	m := newMachine(t, system{name: "macOS", uname: "Darwin"}, "")
	out := fails(t, unpacked(t), m.env(), "./install.sh")
	if !strings.Contains(out, "Moviestracker DMG") {
		t.Errorf("output does not point to the DMG:\n%s", out)
	}
}

func TestAnArchiveForAnotherCPUIsRefusedAndTheRightOneNamed(t *testing.T) {
	other := map[string]string{"amd64": "arm64", "arm64": "amd64"}[runtime.GOARCH]
	archive, _ := releaseFor(t, "linux", other)
	into := t.TempDir()
	untar(t, archive, into)
	m := newMachine(t, linux, "")
	out := fails(t, filepath.Join(into, strings.TrimSuffix(filepath.Base(archive), ".tar.gz")), m.env(),
		"./install.sh", "--prefix", t.TempDir(), "--no-service", "--without-gstreamer")
	if want := "moviestracker_" + version + "_linux_" + runtime.GOARCH + ".tar.gz"; !strings.Contains(out, want) {
		t.Errorf("output does not name %s:\n%s", want, out)
	}
}

func TestInstallFromTheSourceFolderExplainsHowToGetAnArchive(t *testing.T) {
	m := newMachine(t, linux, "")
	out := fails(t, filepath.Join(repoRoot(t), "packaging"), m.env(), "./install.sh", "--prefix", t.TempDir(), "--no-service", "--without-gstreamer")
	for _, want := range []string{"moviestracker and torrserver are not next to install.sh", "make release VERSION=v0.1.0"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestAReleaseCarriesTheSharedTMDBKey(t *testing.T) {
	archive, _ := release(t)
	into := t.TempDir()
	untar(t, archive, into)
	dir := filepath.Join(into, strings.TrimSuffix(filepath.Base(archive), ".tar.gz"))
	bin, err := os.ReadFile(filepath.Join(dir, "moviestracker")) // #nosec G304 -- the archive the test built
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(bin), testSharedTMDBKey) {
		t.Error("the release binary lacks the shared TMDB key it was built with")
	}
}
