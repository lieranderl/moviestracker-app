// Package gstinstall downloads GStreamer for the macOS app's TorrServer: it
// runs the app's install-gstreamer.sh in the background and reports its
// progress, so a web page can show it.
package gstinstall

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Phase is where an install is.
type Phase string

// Phases of an install.
const (
	Idle        Phase = ""
	Downloading Phase = "downloading"
	Installing  Phase = "installing" // unpacking the download
	Starting    Phase = "starting"   // TorrServer restarts to load it
	Done        Phase = "done"
	Failed      Phase = "failed"
)

// Status is what a page shows about GStreamer.
type Status struct {
	Phase             Phase
	Downloaded, Total int64  // bytes of the download
	Error             string // why the last install failed
	Installed         string // the downloaded GStreamer's version, "" when none
	Pinned            string // the version this Moviestracker installs
}

// UpdateAvailable reports an earlier download older than the pinned version.
func (s Status) UpdateAvailable() bool {
	return s.Installed != "" && s.Pinned != "" && s.Installed != s.Pinned
}

// Running reports an install in progress.
func (s Status) Running() bool {
	return s.Phase == Downloading || s.Phase == Installing || s.Phase == Starting
}

// Installer runs install-gstreamer.sh into root. It is safe for concurrent use.
type Installer struct {
	script, root string
	use          func(ctx context.Context, dir string) error

	check        func(ctx context.Context) bool // TorrServer runs with GStreamer
	checkEvery   time.Duration
	checkTimeout time.Duration

	ctx    context.Context
	cancel context.CancelFunc

	pinnedOnce sync.Once
	pinned     string
	size       int64

	mu     sync.Mutex
	phase  Phase
	errMsg string
	seen   bool // the download has appeared: once gone, it is unpacking
}

// New makes an installer for script, which installs into root/<version>.
// use, when not nil, is called with the new folder once an install is done:
// it points TorrServer at it.
func New(script, root string, use func(ctx context.Context, dir string) error, opts ...Option) *Installer {
	ctx, cancel := context.WithCancel(context.Background())
	i := &Installer{script: script, root: root, use: use, ctx: ctx, cancel: cancel}
	for _, opt := range opts {
		opt(i)
	}
	return i
}

// Option configures an Installer.
type Option func(*Installer)

// WithCheck makes an install ready only once check reports that TorrServer
// runs with the new GStreamer, asked every interval for up to timeout.
func WithCheck(check func(ctx context.Context) bool, every, timeout time.Duration) Option {
	return func(i *Installer) { i.check, i.checkEvery, i.checkTimeout = check, every, timeout }
}

// Close stops an install in progress.
func (i *Installer) Close() { i.cancel() }

// InstalledIn returns the GStreamer folder under root, newest version first.
func InstalledIn(root string) (string, bool) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", false
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	for _, name := range names {
		dir := filepath.Join(root, name)
		if _, err := os.Stat(filepath.Join(dir, "lib", "libgstreamer-1.0.0.dylib")); err == nil {
			return dir, true
		}
	}
	return "", false
}

// Status reports the install and what is installed.
func (i *Installer) Status() Status {
	i.pinnedOnce.Do(i.readPinned)
	i.mu.Lock()
	defer i.mu.Unlock()
	st := Status{Phase: i.phase, Error: i.errMsg, Pinned: i.pinned, Total: i.size}
	if dir, ok := InstalledIn(i.root); ok {
		st.Installed = filepath.Base(dir)
	}
	if st.Phase == Downloading {
		info, err := os.Stat(filepath.Join(i.root, ".download", "gstreamer.pkg"))
		_, unpacking := os.Stat(filepath.Join(i.root, ".download", "x"))
		switch {
		case unpacking == nil:
			st.Phase = Installing
		case err != nil && !i.seen:
			// Not started writing yet.
		case err != nil || (st.Total > 0 && info.Size() >= st.Total):
			st.Phase = Installing // the script removes the download once unpacked
		default:
			i.seen = true
			st.Downloaded = info.Size()
		}
	}
	return st
}

// Start begins an install unless one runs; it reports whether it started.
func (i *Installer) Start() bool {
	i.mu.Lock()
	defer i.mu.Unlock()
	if (Status{Phase: i.phase}).Running() {
		return false
	}
	i.phase, i.errMsg, i.seen = Downloading, "", false
	go i.run()
	return true
}

// Dismiss forgets a finished install, so its "ready" message is closed; a
// running or failed one is left as it is.
func (i *Installer) Dismiss() {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.phase == Done {
		i.phase = Idle
	}
}

func (i *Installer) run() {
	cmd := exec.CommandContext(i.ctx, i.script, i.root) // #nosec G204 -- Moviestracker's own install script
	// Its own process group, so stopping it stops its curl too.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		dir, ok := InstalledIn(i.root)
		switch {
		case !ok:
			err = errors.New("the install finished without GStreamer")
		default:
			i.setPhase(Starting)
			if i.use != nil {
				err = i.use(i.ctx, dir)
			}
			if err == nil {
				err = i.waitForTorrServer()
			}
		}
	} else if msg := lastLine(stderr.String()); msg != "" {
		err = errors.New(strings.TrimPrefix(msg, "install-gstreamer: "))
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if err != nil {
		i.phase, i.errMsg = Failed, err.Error()
		return
	}
	i.phase = Done
}

func (i *Installer) setPhase(p Phase) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.phase = p
}

// waitForTorrServer waits until TorrServer runs with GStreamer.
func (i *Installer) waitForTorrServer() error {
	if i.check == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(i.ctx, i.checkTimeout)
	defer cancel()
	tick := time.NewTicker(i.checkEvery)
	defer tick.Stop()
	for {
		if i.check(ctx) {
			return nil
		}
		select {
		case <-ctx.Done():
			return errors.New("TorrServer did not start with GStreamer; see Show Logs in the Moviestracker menu")
		case <-tick.C:
		}
	}
}

// readPinned asks the script which version it installs and its size.
func (i *Installer) readPinned() {
	out := func(arg string) string {
		b, err := exec.Command(i.script, arg).Output() // #nosec G204 -- Moviestracker's own install script
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	i.pinned = out("--version")
	i.size, _ = strconv.ParseInt(out("--size"), 10, 64)
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
