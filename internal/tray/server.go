// Package tray is the part of the Windows tray app that is not Windows: it
// runs moviestracker-server while the user is signed in, restarts it when it
// stops, and says what it is doing, for the tray menu to show. The macOS
// menu bar app (macos/Moviestracker) does the same in Swift.
package tray

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// State is what the tray knows about the server.
type State string

// Server states.
const (
	Stopped       State = "stopped"
	Starting      State = "starting"
	Running       State = "running"
	OtherInstance State = "other" // another Moviestracker already answers on the port
)

// Status is the server's state and, when stopped, why.
type Status struct {
	State  State
	Reason string
}

// Config describes the server to run.
type Config struct {
	// Program is moviestracker-server.
	Program string
	// DataDir is Moviestracker's data directory; its log goes there too.
	DataDir string
	// Port is where the server listens, on every interface.
	Port int
	// Env is added to the server's environment.
	Env []string
	// HealthEvery is how often /healthz is asked (3s when zero).
	HealthEvery time.Duration
	// MinBackoff and MaxBackoff bound the wait before a stopped server is
	// started again; it doubles every time (2s and 60s when zero).
	MinBackoff, MaxBackoff time.Duration
	// StopTimeout is how long Stop waits before it kills the server (15s).
	StopTimeout time.Duration
	// OnChange is called, on its own goroutine, whenever the status changes.
	OnChange func(Status)
}

// Server runs one moviestracker-server. It is safe for concurrent use.
type Server struct {
	cfg    Config
	health *http.Client

	mu     sync.Mutex
	status Status
	stop   chan struct{} // closed by Stop; nil when not started
	done   chan struct{} // closed when the loop has ended
}

// NewServer returns a stopped server for cfg.
func NewServer(cfg Config) *Server {
	if cfg.HealthEvery == 0 {
		cfg.HealthEvery = 3 * time.Second
	}
	if cfg.MinBackoff == 0 {
		cfg.MinBackoff = 2 * time.Second
	}
	if cfg.MaxBackoff == 0 {
		cfg.MaxBackoff = time.Minute
	}
	if cfg.StopTimeout == 0 {
		cfg.StopTimeout = 15 * time.Second
	}
	return &Server{
		cfg:    cfg,
		health: &http.Client{Timeout: 2 * time.Second},
		status: Status{State: Stopped},
	}
}

// Status returns what the server is doing.
func (s *Server) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Start runs the server in the background until Stop, unless another
// Moviestracker already answers on the port: then it waits for that one to
// go away and takes over.
func (s *Server) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stop != nil {
		return
	}
	s.stop, s.done = make(chan struct{}), make(chan struct{})
	go s.loop(s.stop, s.done)
}

// Stop ends the server and waits until it has exited: it closes the
// server's input, which stops it, and kills it after StopTimeout.
func (s *Server) Stop() {
	s.mu.Lock()
	stop, done := s.stop, s.done
	s.stop, s.done = nil, nil
	s.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-done
	s.set(Status{State: Stopped, Reason: "stopped"})
}

// Restart stops the server and starts it again.
func (s *Server) Restart() {
	s.Stop()
	s.Start()
}

func (s *Server) set(st Status) {
	s.mu.Lock()
	changed := s.status != st
	s.status = st
	s.mu.Unlock()
	if changed && s.cfg.OnChange != nil {
		go s.cfg.OnChange(st)
	}
}

// loop keeps one server running until stop is closed.
func (s *Server) loop(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	backoff := s.cfg.MinBackoff
	for {
		if s.healthy() {
			Logf(s.cfg.DataDir, "port %d already answers: another Moviestracker runs", s.cfg.Port)
			s.set(Status{State: OtherInstance})
			if !s.waitUntilGone(stop) {
				return
			}
			continue
		}
		p, err := s.launch()
		if err != nil {
			Logf(s.cfg.DataDir, "cannot start moviestracker-server: %v", err)
			s.set(Status{State: Stopped, Reason: "cannot start: " + err.Error()})
		} else {
			s.set(Status{State: Starting})
			started := time.Now()
			if !s.watch(p, stop) {
				return
			}
			if time.Since(started) > time.Minute {
				backoff = s.cfg.MinBackoff // it had been running fine
			}
			Logf(s.cfg.DataDir, "moviestracker-server exited (%s); starting again in %s", p.exitReason(), backoff)
			s.set(Status{State: Stopped, Reason: fmt.Sprintf("stopped (%s); starting again in %s", p.exitReason(), backoff.Round(time.Second))})
		}
		select {
		case <-stop:
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, s.cfg.MaxBackoff)
	}
}

// waitUntilGone polls until nothing answers on the port (true) or stop.
func (s *Server) waitUntilGone(stop <-chan struct{}) bool {
	tick := time.NewTicker(s.cfg.HealthEvery)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return false
		case <-tick.C:
			if !s.healthy() {
				return true
			}
		}
	}
}

// watch follows a running server until it exits (true) or stop, which ends
// it (false).
func (s *Server) watch(p *process, stop <-chan struct{}) bool {
	tick := time.NewTicker(s.cfg.HealthEvery)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			p.end(s.cfg.StopTimeout)
			return false
		case <-p.exited:
			return true
		case <-tick.C:
			if s.healthy() {
				s.set(Status{State: Running})
			} else {
				s.set(Status{State: Starting})
			}
		}
	}
}

func (s *Server) healthy() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(s.cfg.Port)+"/healthz", nil)
	if err != nil {
		return false
	}
	resp, err := s.health.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16))
	return resp.StatusCode == http.StatusOK && strings.HasPrefix(string(body), "ok")
}

// process is one run of the server.
type process struct {
	cmd    *exec.Cmd
	input  io.Closer     // the server stops when this closes
	exited chan struct{} // closed when it has exited
}

func (s *Server) launch() (*process, error) {
	if err := os.MkdirAll(s.cfg.DataDir, 0o700); err != nil {
		return nil, err
	}
	RotateLog(s.cfg.DataDir)
	log, err := os.OpenFile(LogPath(s.cfg.DataDir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(s.cfg.Program) // #nosec G204 -- moviestracker-server next to the tray app
	cmd.Env = append(os.Environ(), s.cfg.Env...)
	cmd.Env = append(cmd.Env,
		"MT_LISTEN=:"+strconv.Itoa(s.cfg.Port),
		"MT_DATA_DIR="+s.cfg.DataDir,
		"MT_STOP_WITH_STDIN=1",
	)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = sysProcAttr()
	input, err := cmd.StdinPipe()
	if err != nil {
		_ = log.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		_ = log.Close()
		return nil, err
	}
	p := &process{cmd: cmd, input: input, exited: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		_ = log.Close()
		close(p.exited)
	}()
	return p, nil
}

// end closes the server's input, which stops it, and kills it if it has not
// exited after timeout.
func (p *process) end(timeout time.Duration) {
	_ = p.input.Close()
	select {
	case <-p.exited:
	case <-time.After(timeout):
		_ = p.cmd.Process.Kill()
		<-p.exited
	}
}

func (p *process) exitReason() string {
	if p.cmd.ProcessState == nil {
		return "unknown"
	}
	return p.cmd.ProcessState.String()
}

// LogPath is the server's log, which the tray writes to too.
func LogPath(dataDir string) string { return filepath.Join(dataDir, "moviestracker.log") }

// RotateLog keeps the log under 10 MB: a bigger one becomes
// moviestracker.log.1 (replacing the previous one) and the log starts afresh.
func RotateLog(dataDir string) {
	path := LogPath(dataDir)
	if info, err := os.Stat(path); err != nil || info.Size() <= 10<<20 {
		return
	}
	if os.Rename(path, path+".1") == nil {
		_ = os.WriteFile(path, nil, 0o600)
	}
}

// LogWriter writes each line given to it to the log, like Logf.
func LogWriter(dataDir string) io.Writer { return logWriter(dataDir) }

type logWriter string

func (dir logWriter) Write(p []byte) (int, error) {
	Logf(string(dir), "%s", strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

// Logf adds a line from the tray app to the log.
func Logf(dataDir, format string, args ...any) {
	f, err := os.OpenFile(LogPath(dataDir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "%s tray: %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
}
