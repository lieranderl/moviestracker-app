package packaging_test

import (
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
)

var systems = []system{
	{"Debian", "Linux", "apt-get", "apt-get install -y gstreamer1.0-tools gstreamer1.0-plugins-base gstreamer1.0-plugins-good gstreamer1.0-plugins-bad gstreamer1.0-plugins-ugly gstreamer1.0-libav"},
	{"Fedora", "Linux", "dnf", "dnf install -y gstreamer1 gstreamer1-plugins-base gstreamer1-plugins-good gstreamer1-plugins-bad-free gstreamer1-plugins-ugly-free gstreamer1-plugin-libav"},
	{"Arch", "Linux", "pacman", "pacman -S --needed --noconfirm gstreamer gst-plugins-base gst-plugins-good gst-plugins-bad gst-plugins-ugly gst-libav"},
}

func TestWithGStreamerTheInstallerAddsItWithTheSystemsPackageManager(t *testing.T) {
	dir := unpacked(t)
	for _, sys := range systems {
		t.Run(sys.name, func(t *testing.T) {
			m := newMachine(t, sys, "1.24.2")
			out := run(t, dir, m.env(), "./install.sh", "--prefix", t.TempDir(), "--no-service", "--with-gstreamer")
			if !strings.Contains(m.calls(), sys.command) {
				t.Errorf("package manager calls:\n%s\nwant %q", m.calls(), sys.command)
			}
			if !strings.Contains(out, "GStreamer 1.24.2 is installed: MKV files play in the browser.") {
				t.Errorf("output lacks the result:\n%s", out)
			}
		})
	}
}

func TestWithNoOneToAskTheInstallerShowsHowToAddGStreamerLater(t *testing.T) {
	dir := unpacked(t)
	for _, sys := range systems {
		t.Run(sys.name, func(t *testing.T) {
			m := newMachine(t, sys, "1.24.2")
			out := run(t, dir, m.env(), "./install.sh", "--prefix", t.TempDir(), "--no-service")
			if m.calls() != "" {
				t.Errorf("installed without asking:\n%s", m.calls())
			}
			for _, want := range []string{"Browsers cannot play MKV files", "To add it (a few hundred MB):", sys.command} {
				if !strings.Contains(out, want) {
					t.Errorf("output lacks %q:\n%s", want, out)
				}
			}
		})
	}
}

func TestWithoutGStreamerTheInstallerNeitherAsksNorInstalls(t *testing.T) {
	m := newMachine(t, systems[0], "1.24.2")
	out := run(t, unpacked(t), m.env(), "./install.sh", "--prefix", t.TempDir(), "--no-service", "--without-gstreamer")
	if m.calls() != "" || strings.Contains(out, "MKV") {
		t.Errorf("calls %q, output:\n%s\nwant GStreamer left alone", m.calls(), out)
	}
}

func TestAGStreamerAlreadyInstalledIsKept(t *testing.T) {
	m := newMachine(t, systems[0], "1.26.0")
	m.fake(t, "gst-inspect-1.0", "echo 'gst-inspect-1.0 version 1.24.2'; echo 'GStreamer 1.24.2'")
	out := run(t, unpacked(t), m.env(), "./install.sh", "--prefix", t.TempDir(), "--no-service", "--with-gstreamer")
	if m.calls() != "" {
		t.Errorf("reinstalled GStreamer:\n%s", m.calls())
	}
	if !strings.Contains(out, "GStreamer 1.24.2 found: MKV files play in the browser.") {
		t.Errorf("output:\n%s", out)
	}
}

func TestASystemWithTooOldAGStreamerIsToldTheRequirements(t *testing.T) {
	m := newMachine(t, systems[0], "1.20.3")
	out := run(t, unpacked(t), m.env(), "./install.sh", "--prefix", t.TempDir(), "--no-service", "--with-gstreamer")
	for _, want := range []string{"GStreamer (1.20.3) is too old", "GStreamer 1.22 or newer", "Ubuntu 24.04+"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestWithoutAPackageManagerTheInstallerSaysWhatIsNeeded(t *testing.T) {
	m := newMachine(t, linux, "")
	out := run(t, unpacked(t), m.env(), "./install.sh", "--prefix", t.TempDir(), "--no-service", "--with-gstreamer")
	if want := "Browser playback needs GStreamer 1.22 or newer"; !strings.Contains(out, want) {
		t.Errorf("output lacks %q:\n%s", want, out)
	}
}

// answering runs install.sh on a terminal (through script(1)) and types answer.
func answering(t *testing.T, dir string, m machine, answer string, args ...string) string {
	t.Helper()
	script, err := exec.LookPath("script")
	if err != nil {
		t.Skip("no script(1) to give the installer a terminal")
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" {
		cmd = exec.Command(script, append([]string{"-q", "/dev/null", "./install.sh"}, args...)...) // #nosec G204 -- fixed test command
	} else {
		cmd = exec.Command(script, "-qec", "./install.sh "+strings.Join(args, " "), "/dev/null") // #nosec G204 -- fixed test command
	}
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), append(m.env(), "SHELL=/bin/sh")...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	// Type the answer once the question shows; stdin stays open until the end,
	// as a person's keyboard would.
	out := &typist{prompt: "[Y/n]", answer: answer, to: stdin}
	cmd.Stdout, cmd.Stderr = out, out
	err = cmd.Run()
	_ = stdin.Close()
	if err != nil {
		t.Fatalf("install.sh on a terminal: %v\n%s", err, out.buf.String())
	}
	return out.buf.String()
}

type typist struct {
	mu     sync.Mutex
	buf    strings.Builder
	prompt string
	answer string
	typed  bool
	to     io.Writer
}

func (w *typist) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	if !w.typed && strings.Contains(w.buf.String(), w.prompt) {
		w.typed = true
		_, _ = io.WriteString(w.to, w.answer)
	}
	return len(p), nil
}

func TestOnATerminalTheInstallerAsksAndYesIsTheDefault(t *testing.T) {
	dir := unpacked(t)
	for answer, installs := range map[string]bool{"\n": true, "y\n": true, "n\n": false} {
		t.Run(strings.TrimSpace(answer), func(t *testing.T) {
			m := newMachine(t, systems[0], "1.24.2")
			out := answering(t, dir, m, answer, "--prefix", t.TempDir(), "--no-service")
			if !strings.Contains(out, "Browsers cannot play MKV files") || !strings.Contains(out, "[Y/n]") {
				t.Errorf("no explained question:\n%s", out)
			}
			if got := strings.Contains(m.calls(), systems[0].command); got != installs {
				t.Errorf("answer %q installed = %v, want %v; output:\n%s", answer, got, installs, out)
			}
		})
	}
}

func TestUninstallingOffersToRemoveTheGStreamerTheInstallerAdded(t *testing.T) {
	dir := unpacked(t)
	sys := systems[0]
	remove := "apt-get remove gstreamer1.0-tools gstreamer1.0-plugins-base gstreamer1.0-plugins-good gstreamer1.0-plugins-bad gstreamer1.0-plugins-ugly gstreamer1.0-libav"

	m := newMachine(t, sys, "1.24.2")
	prefix := t.TempDir()
	run(t, dir, m.env(), "./install.sh", "--prefix", prefix, "--no-service", "--with-gstreamer")
	out := run(t, dir, m.env(), "./install.sh", "--prefix", prefix, "--no-service", "--uninstall")
	if !strings.Contains(out, remove) || strings.Contains(m.calls(), "remove") {
		t.Errorf("with no one to ask, uninstall should show how to remove GStreamer, not remove it:\n%s\ncalls:\n%s", out, m.calls())
	}

	m = newMachine(t, sys, "1.24.2")
	prefix = t.TempDir()
	run(t, dir, m.env(), "./install.sh", "--prefix", prefix, "--no-service", "--with-gstreamer")
	out = answering(t, dir, m, "\n", "--prefix", prefix, "--no-service", "--uninstall")
	if !strings.Contains(out, "Remove it too?") || !strings.Contains(m.calls(), remove) {
		t.Errorf("on a terminal uninstall should offer to remove GStreamer:\n%s\ncalls:\n%s", out, m.calls())
	}

	m = newMachine(t, sys, "1.24.2")
	prefix = t.TempDir()
	run(t, dir, m.env(), "./install.sh", "--prefix", prefix, "--no-service", "--without-gstreamer")
	if out := run(t, dir, m.env(), "./install.sh", "--prefix", prefix, "--no-service", "--uninstall"); strings.Contains(out, "apt-get remove") {
		t.Errorf("GStreamer the installer did not add is not its to remove:\n%s", out)
	}
}
