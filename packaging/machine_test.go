// The Linux archive and the Mac app; scripts/ci/windows-install-e2e.ps1
// checks the Windows installer.

//go:build unix

package packaging_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// system is a machine the installer can meet: its kernel and the package
// manager that adds GStreamer to it.
type system struct {
	name    string
	uname   string
	manager string // the fake package manager on PATH; "" for none
	command string // what the installer must run
}

// machine is a sealed PATH: the few real tools install.sh uses, plus fakes of
// uname, sudo, the package manager and (once "installed") gst-inspect-1.0.
type machine struct {
	bin string
	log string // every package manager call, one per line
}

func newMachine(t *testing.T, sys system, gstVersion string) machine {
	t.Helper()
	m := machine{bin: t.TempDir(), log: filepath.Join(t.TempDir(), "calls.log")}
	for _, tool := range []string{"sh", "sed", "awk", "dirname", "install", "cp", "cat", "chmod", "rm", "mkdir", "id", "grep", "tr"} {
		real, err := exec.LookPath(tool)
		if err != nil {
			t.Fatalf("this machine lacks %s: %v", tool, err)
		}
		if err := os.Symlink(real, filepath.Join(m.bin, tool)); err != nil {
			t.Fatal(err)
		}
	}
	m.fake(t, "uname", `case "$1" in -m) echo `+unameArch(sys.uname)+` ;; *) echo `+sys.uname+` ;; esac`)
	m.fake(t, "sudo", `exec "$@"`)
	if sys.manager != "" {
		// Installing writes a gst-inspect-1.0 that reports gstVersion.
		m.fake(t, sys.manager, `echo "${0##*/} $*" >>`+m.log+`
case "$*" in *install*|-S*) cat >`+filepath.Join(m.bin, "gst-inspect-1.0")+` <<'GST'
#!/bin/sh
echo "gst-inspect-1.0 version `+gstVersion+`"
echo "GStreamer `+gstVersion+`"
GST
chmod +x `+filepath.Join(m.bin, "gst-inspect-1.0")+` ;; esac`)
	}
	return m
}

func (m machine) fake(t *testing.T, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(m.bin, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil { // #nosec G306 -- an executable fixture
		t.Fatal(err)
	}
}

func (m machine) calls() string {
	raw, _ := os.ReadFile(m.log) // #nosec G304 -- the test's own log
	return string(raw)
}

func (m machine) env() []string { return []string{"PATH=" + m.bin} }

// unameArch is what uname -m says for this CPU on kernel.
func unameArch(kernel string) string {
	switch {
	case runtime.GOARCH == "amd64":
		return "x86_64"
	case kernel == "Linux":
		return "aarch64"
	}
	return "arm64"
}

// unpacked is the Linux archive for this CPU, unpacked.
func unpacked(t *testing.T) string {
	t.Helper()
	archive, _ := release(t)
	into := t.TempDir()
	untar(t, archive, into)
	return filepath.Join(into, strings.TrimSuffix(filepath.Base(archive), ".tar.gz"))
}

// linux is a Linux machine without a package manager.
var linux = system{name: "Linux", uname: "Linux"}

// fails runs install-like commands that must fail and returns their output.
func fails(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...) // #nosec G204 -- the tests' own fixed commands
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("%s %v succeeded, want a refusal:\n%s", name, args, out)
	}
	return string(out)
}
