// Package engine runs TorrServer as a child process of Moviestracker: bound
// to loopback, behind generated credentials, restarted when it crashes and
// stopped with Moviestracker.
package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// DefaultPort is where the engine listens unless that port is taken. It is
// deliberately not TorrServer's 8090, so an existing install keeps working.
const DefaultPort = 18090

// engineUser is the account Moviestracker uses on its engine.
const engineUser = "moviestracker"

// State is what the engine is doing.
type State string

// Engine states.
const (
	Stopped    State = "stopped"
	Starting   State = "starting"
	Running    State = "running"
	Restarting State = "restarting"
)

// Config describes the engine to run.
type Config struct {
	// Binary is the TorrServer executable.
	Binary string
	// Dir is TorrServer's data directory (--path): its database, settings,
	// accs.db and logs.
	Dir string
	// Port is the preferred loopback port (DefaultPort when zero).
	Port int
	// Env is added to the engine's environment.
	Env []string
	// ReadyTimeout bounds how long a start may take (30s when zero).
	ReadyTimeout time.Duration
	// MinBackoff and MaxBackoff bound the wait before a crashed engine is
	// restarted; the wait doubles after every crash (1s and 60s when zero).
	MinBackoff, MaxBackoff time.Duration
	// Options are the command-line-only settings (see SetOptions).
	Options Options
	// FirstStart runs once in the life of an engine directory, after its
	// first successful start: the place for Moviestracker's own defaults.
	// It runs again on the next start if it fails.
	FirstStart func(ctx context.Context, url, user, password string) error
	// OnReady is called whenever the engine (re)starts and answers, with the
	// address and credentials to reach it.
	OnReady func(url, user, password string)
}

// Status is a snapshot of the engine.
type Status struct {
	State     State
	Version   string
	PID       int
	Port      int
	Restarts  int
	LastError string
	StartedAt time.Time
}

// Supervisor starts, watches and stops one engine. It is safe for concurrent use.
type Supervisor struct {
	cfg      Config
	password string
	http     *http.Client

	mu     sync.Mutex
	status Status
	run    *run // the current launch; nil when stopped
}

// run is one launch of the engine and the goroutine watching it.
type run struct {
	cmd    *exec.Cmd
	port   int
	exited chan struct{} // closed when the process has exited
	stop   chan struct{} // closed by Stop
	done   chan struct{} // closed when the watcher has finished
}

// New returns a stopped supervisor for cfg.
func New(cfg Config) *Supervisor {
	if cfg.Port == 0 {
		cfg.Port = DefaultPort
	}
	if cfg.ReadyTimeout == 0 {
		cfg.ReadyTimeout = 30 * time.Second
	}
	if cfg.MinBackoff == 0 {
		cfg.MinBackoff = time.Second
	}
	if cfg.MaxBackoff == 0 {
		cfg.MaxBackoff = time.Minute
	}
	return &Supervisor{
		cfg:    cfg,
		http:   &http.Client{Timeout: 3 * time.Second},
		status: Status{State: Stopped},
	}
}

// Status returns a snapshot of the engine.
func (s *Supervisor) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Endpoint returns the engine's address and the credentials to reach it.
func (s *Supervisor) Endpoint() (url, user, password string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return baseURL(s.status.Port), engineUser, s.password
}

// Start launches the engine and returns once it answers. A crash after that
// is handled by restarting it until Stop.
func (s *Supervisor) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.run != nil {
		s.mu.Unlock()
		return errors.New("engine already started")
	}
	s.status = Status{State: Starting}
	s.mu.Unlock()

	fail := func(err error) error {
		s.mu.Lock()
		s.status.State, s.status.LastError = Stopped, err.Error()
		s.mu.Unlock()
		return err
	}
	if _, err := os.Stat(s.cfg.Binary); err != nil {
		return fail(fmt.Errorf("TorrServer program not found: %w", err))
	}
	if err := os.MkdirAll(s.cfg.Dir, 0o700); err != nil {
		return fail(fmt.Errorf("create engine directory: %w", err))
	}
	password, err := credentials(s.cfg.Dir)
	if err != nil {
		return fail(err)
	}
	s.mu.Lock()
	s.password = password
	s.mu.Unlock()

	r, err := s.launch(ctx, nil)
	if err != nil {
		return fail(err)
	}
	s.mu.Lock()
	s.run = r
	s.mu.Unlock()
	s.firstStart(ctx)
	go s.watch(r) // #nosec G118 -- the watcher outlives Start's context: the engine runs until Stop
	return nil
}

// firstStartMarker records in the engine directory that FirstStart has run.
const firstStartMarker = "moviestracker-setup.done"

// firstStart runs cfg.FirstStart once per engine directory.
func (s *Supervisor) firstStart(ctx context.Context) {
	marker := filepath.Join(s.cfg.Dir, firstStartMarker)
	if s.cfg.FirstStart == nil {
		return
	}
	if _, err := os.Stat(marker); err == nil {
		return
	}
	url, user, password := s.Endpoint()
	if err := s.cfg.FirstStart(ctx, url, user, password); err != nil {
		slog.Warn("engine first-start setup failed; will retry on the next start", "error", err)
		return
	}
	if err := os.WriteFile(marker, []byte("Moviestracker applied its defaults to this engine.\n"), 0o600); err != nil {
		slog.Warn("cannot record engine first-start setup", "error", err)
	}
}

// Stop ends the engine: SIGTERM (a /shutdown request on Windows), then a kill
// after 10 seconds.
func (s *Supervisor) Stop() error {
	s.mu.Lock()
	r := s.run
	s.run = nil
	s.mu.Unlock()
	if r == nil {
		return nil
	}
	close(r.stop)
	<-r.done
	s.mu.Lock()
	s.status.State, s.status.PID = Stopped, 0
	s.mu.Unlock()
	return nil
}

// Restart stops the engine and starts it again.
func (s *Supervisor) Restart(ctx context.Context) error {
	if err := s.Stop(); err != nil {
		return err
	}
	return s.Start(ctx)
}

// launch starts one engine process and waits until it answers. stop, when
// not nil, aborts the wait.
func (s *Supervisor) launch(ctx context.Context, stop <-chan struct{}) (*run, error) {
	port, err := s.choosePort(ctx)
	if err != nil {
		return nil, err
	}
	out, err := os.OpenFile(filepath.Join(s.cfg.Dir, "engine.out"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open engine output: %w", err)
	}
	args := []string{
		"--ip", "127.0.0.1",
		"--port", strconv.Itoa(port),
		"--path", s.cfg.Dir,
		"--logpath", filepath.Join(s.cfg.Dir, "torrserver.log"),
		"--httpauth",
	}
	args = append(args, s.Options().args()...)
	cmd := exec.Command(s.cfg.Binary, args...) // #nosec G204 -- the configured TorrServer program; flags come from typed Options
	cmd.Dir = s.cfg.Dir
	cmd.Env = append(os.Environ(), s.cfg.Env...)
	cmd.Stdout, cmd.Stderr = out, out
	cmd.SysProcAttr = sysProcAttr()
	if err := cmd.Start(); err != nil {
		_ = out.Close()
		return nil, fmt.Errorf("start TorrServer: %w", err)
	}
	tieToMoviestracker(cmd.Process)
	r := &run{cmd: cmd, port: port, exited: make(chan struct{}), stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		_ = out.Close()
		close(r.exited)
	}()

	version, err := s.waitReady(ctx, port, r.exited, stop)
	if err != nil {
		s.terminate(r)
		return nil, err
	}
	s.mu.Lock()
	s.status.State = Running
	s.status.Version, s.status.PID, s.status.Port = version, cmd.Process.Pid, port
	s.status.StartedAt = time.Now()
	password := s.password
	s.mu.Unlock()
	if s.cfg.OnReady != nil {
		s.cfg.OnReady(baseURL(port), engineUser, password)
	}
	slog.Info("engine running", "version", version, "port", port, "pid", cmd.Process.Pid)
	return r, nil
}

// waitReady polls the engine until it answers with our credentials.
func (s *Supervisor) waitReady(ctx context.Context, port int, exited, stop <-chan struct{}) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.ReadyTimeout)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		if version, err := s.version(ctx, port); err == nil && s.authorized(ctx, port, engineUser, s.password) {
			return version, nil
		}
		select {
		case <-exited:
			return "", fmt.Errorf("TorrServer exited while starting; see %s", filepath.Join(s.cfg.Dir, "engine.out"))
		case <-stop:
			return "", errors.New("stopped while starting")
		case <-ctx.Done():
			return "", fmt.Errorf("TorrServer did not answer within %s", s.cfg.ReadyTimeout)
		case <-tick.C:
		}
	}
}

// watch restarts the engine of r whenever it exits, until Stop.
func (s *Supervisor) watch(r *run) {
	backoff := s.cfg.MinBackoff
	stop := r.stop
	for {
		select {
		case <-stop:
			s.terminate(r)
			close(r.done)
			return
		case <-r.exited:
		}
		s.mu.Lock()
		if time.Since(s.status.StartedAt) > time.Minute {
			backoff = s.cfg.MinBackoff // it had been running fine
		}
		s.status.Restarts++
		s.status.State, s.status.PID = Restarting, 0
		s.status.LastError = "TorrServer exited unexpectedly: " + exitReason(r.cmd)
		s.mu.Unlock()
		slog.Warn("engine exited; restarting", "in", backoff, "reason", exitReason(r.cmd))

		for {
			select {
			case <-stop:
				close(r.done)
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, s.cfg.MaxBackoff)
			next, err := s.launch(context.Background(), stop)
			if err == nil {
				// Keep r (and its channels) as the handle Stop knows about.
				r.cmd, r.port, r.exited = next.cmd, next.port, next.exited
				break
			}
			s.mu.Lock()
			s.status.Restarts++
			s.status.LastError = err.Error()
			s.mu.Unlock()
		}
	}
}

// terminate stops the process of r: SIGTERM, then a kill after 10 seconds.
// Where there is no SIGTERM (Windows), it asks the engine to shut down.
func (s *Supervisor) terminate(r *run) {
	select {
	case <-r.exited:
		return
	default:
	}
	if err := r.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		s.shutdown(ctx, r.port)
		cancel()
	}
	select {
	case <-r.exited:
	case <-time.After(10 * time.Second):
		_ = r.cmd.Process.Kill()
		<-r.exited
	}
}

func exitReason(cmd *exec.Cmd) string {
	if cmd.ProcessState == nil {
		return "unknown"
	}
	return cmd.ProcessState.String()
}

// choosePort returns the preferred port when it is free or held by an engine
// of ours left by an earlier run (which is shut down), and a free port when
// something else holds it.
func (s *Supervisor) choosePort(ctx context.Context) (int, error) {
	port := s.cfg.Port
	if portFree(port) {
		return port, nil
	}
	if s.ownEngineOn(ctx, port) {
		slog.Info("shutting down an engine left by an earlier run", "port", port)
		s.shutdown(ctx, port)
		deadline := time.Now().Add(10 * time.Second)
		for !portFree(port) {
			if time.Now().After(deadline) {
				return 0, fmt.Errorf("the engine left on port %d did not stop", port)
			}
			time.Sleep(50 * time.Millisecond)
		}
		return port, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("find a free port: %w", err)
	}
	defer func() { _ = ln.Close() }()
	slog.Info("engine port is taken by another program; using another", "taken", port)
	return ln.Addr().(*net.TCPAddr).Port, nil
}

// ownEngineOn reports whether port is held by a TorrServer that accepts our
// credentials but refuses wrong ones: one started with our accs.db. Anything
// else (such as someone's own TorrServer without auth) is left alone.
func (s *Supervisor) ownEngineOn(ctx context.Context, port int) bool {
	return s.authorized(ctx, port, engineUser, s.password) && !s.authorized(ctx, port, engineUser, s.password+"-wrong")
}

func (s *Supervisor) shutdown(ctx context.Context, port int) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL(port)+"/shutdown", nil)
	if err != nil {
		return
	}
	req.SetBasicAuth(engineUser, s.password)
	if resp, err := s.http.Do(req); err == nil {
		_ = resp.Body.Close()
	}
}

// version reads TorrServer's /echo.
func (s *Supervisor) version(ctx context.Context, port int) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL(port)+"/echo", nil)
	if err != nil {
		return "", err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("echo returned %d", resp.StatusCode)
	}
	return strings.TrimSpace(string(body)), nil
}

// authorized reports whether an authenticated settings read succeeds.
func (s *Supervisor) authorized(ctx context.Context, port int, user, password string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL(port)+"/settings", strings.NewReader(`{"action":"get"}`))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(user, password)
	resp, err := s.http.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func portFree(port int) bool {
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

func baseURL(port int) string {
	return "http://127.0.0.1:" + strconv.Itoa(port)
}

// credentials returns Moviestracker's engine password from accs.db, creating
// the file (owner-only) with a random password on first use.
func credentials(dir string) (string, error) {
	path := filepath.Join(dir, "accs.db")
	accounts := map[string]string{}
	if raw, err := os.ReadFile(path); err == nil { // #nosec G304 -- the engine's own data directory
		_ = json.Unmarshal(raw, &accounts)
		if pw := accounts[engineUser]; len(pw) >= 32 {
			return pw, os.Chmod(path, 0o600)
		}
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate engine password: %w", err)
	}
	accounts[engineUser] = hex.EncodeToString(b)
	raw, err := json.Marshal(accounts)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", fmt.Errorf("write engine credentials: %w", err)
	}
	return accounts[engineUser], os.Chmod(path, 0o600)
}
