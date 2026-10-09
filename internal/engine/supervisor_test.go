package engine_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/engine"
	"github.com/lieranderl/moviestracker-app/internal/engine/enginetest"
)

func TestMain(m *testing.M) {
	enginetest.RunFakeIfRequested()
	holdEngineIfRequested()
	os.Exit(m.Run())
}

// holderEnv makes the test binary a stand-in for Moviestracker: it runs an
// engine in the directory and on the port given ("dir|port") until killed.
const holderEnv = "ENGINE_TEST_HOLDER"

func holdEngineIfRequested() {
	dir, port, ok := strings.Cut(os.Getenv(holderEnv), "|")
	if !ok {
		return
	}
	n, _ := strconv.Atoi(port)
	sup := engine.New(engine.Config{Binary: os.Args[0], Dir: dir, Port: n, Env: []string{enginetest.FakeEnv + "=1", holderEnv + "="}})
	if err := sup.Start(context.Background()); err != nil {
		os.Exit(4)
	}
	select {}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

func newSupervisor(t *testing.T, dir string, port int, env ...string) *engine.Supervisor {
	t.Helper()
	sup := engine.New(engine.Config{
		Binary:       os.Args[0],
		Dir:          dir,
		Port:         port,
		Env:          append([]string{enginetest.FakeEnv + "=1"}, env...),
		ReadyTimeout: 10 * time.Second,
		MinBackoff:   20 * time.Millisecond,
		MaxBackoff:   200 * time.Millisecond,
	})
	t.Cleanup(func() { _ = sup.Stop() })
	return sup
}

func get(t *testing.T, url, user, pass string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url+"/settings", strings.NewReader(`{"action":"get"}`))
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func TestTheEngineRunsOnLoopbackBehindGeneratedCredentials(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)
	sup := newSupervisor(t, dir, port)
	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start(): %v", err)
	}

	st := sup.Status()
	url, user, pass := sup.Endpoint()
	if st.State != engine.Running || st.Version != "MatriX.fake" || st.PID == 0 {
		t.Fatalf("Status() = %+v, want running MatriX.fake with a PID", st)
	}
	if url != "http://127.0.0.1:"+strconv.Itoa(port) {
		t.Errorf("Endpoint() URL = %q, want loopback on port %d", url, port)
	}
	if len(pass) < 32 {
		t.Errorf("generated password %q is too short", pass)
	}
	if code := get(t, url, "", ""); code != http.StatusUnauthorized {
		t.Errorf("engine without credentials: %d, want 401", code)
	}
	if code := get(t, url, user, pass); code != http.StatusOK {
		t.Errorf("engine with credentials: %d, want 200", code)
	}
	info, err := os.Stat(filepath.Join(dir, "accs.db"))
	if err != nil {
		t.Errorf("accs.db: %v", err)
	} else if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 { // Windows has no Unix modes
		t.Errorf("accs.db mode %v; want 0600", info.Mode().Perm())
	}

	started := time.Now()
	if err := sup.Stop(); err != nil {
		t.Fatalf("Stop(): %v", err)
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("Stop() took %s; the engine should stop when asked, not be killed after a timeout", took)
	}
	if code := get(t, url, user, pass); code != 0 {
		t.Errorf("engine still answers after Stop (%d)", code)
	}
	if st := sup.Status(); st.State != engine.Stopped {
		t.Errorf("state after Stop = %q, want stopped", st.State)
	}
}

func TestTheEngineStopsWhenMoviestrackerIsKilled(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("macOS cannot tie a child to its parent; the next start replaces a left engine")
	}
	port := freePort(t)
	holder := exec.Command(os.Args[0], "-test.run=^$") // #nosec G204 G702 -- this test binary
	holder.Env = append(os.Environ(), holderEnv+"="+t.TempDir()+"|"+strconv.Itoa(port))
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = holder.Process.Kill() })
	url := "http://127.0.0.1:" + strconv.Itoa(port)
	answers := func() bool {
		resp, err := http.Get(url + "/echo") // #nosec G107 -- loopback test server
		if err == nil {
			_ = resp.Body.Close()
		}
		return err == nil
	}
	deadline := time.Now().Add(10 * time.Second)
	for !answers() {
		if time.Now().After(deadline) {
			t.Fatal("the engine never started")
		}
		time.Sleep(50 * time.Millisecond)
	}

	_ = holder.Process.Kill()
	_ = holder.Wait()
	deadline = time.Now().Add(5 * time.Second)
	for answers() {
		if time.Now().After(deadline) {
			t.Fatal("the engine still runs after Moviestracker was killed")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestTheSameCredentialsAreUsedAfterARestartOfMoviestracker(t *testing.T) {
	dir := t.TempDir()
	first := newSupervisor(t, dir, freePort(t))
	if err := first.Start(context.Background()); err != nil {
		t.Fatalf("Start(): %v", err)
	}
	_, _, pass1 := first.Endpoint()
	_ = first.Stop()

	second := newSupervisor(t, dir, freePort(t))
	if err := second.Start(context.Background()); err != nil {
		t.Fatalf("Start(): %v", err)
	}
	if _, _, pass2 := second.Endpoint(); pass2 != pass1 {
		t.Error("credentials changed across restarts")
	}
}

func TestACrashedEngineIsRestartedWithBackoff(t *testing.T) {
	sup := newSupervisor(t, t.TempDir(), freePort(t), "FAKE_EXIT_AFTER=150ms")
	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start(): %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for sup.Status().Restarts < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("no restarts after crashes: %+v", sup.Status())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if st := sup.Status(); st.LastError == "" {
		t.Errorf("Status() after a crash has no error: %+v", st)
	}
}

func TestAnEngineThatFailsToStartIsTriedAgain(t *testing.T) {
	sup := newSupervisor(t, t.TempDir(), freePort(t), "FAKE_FAIL_STARTS=2")
	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start() after two failed starts: %v", err)
	}
	st := sup.Status()
	if st.State != engine.Running {
		t.Fatalf("Status() = %+v, want running", st)
	}
	if url, user, pass := sup.Endpoint(); get(t, url, user, pass) != http.StatusOK {
		t.Errorf("the engine does not answer with its credentials at %s", url)
	}
}

func TestAnEngineThatNeverStartsIsReported(t *testing.T) {
	sup := newSupervisor(t, t.TempDir(), freePort(t), "FAKE_FAIL_STARTS=100")
	err := sup.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "engine.out") {
		t.Fatalf("Start() = %v, want an error pointing at engine.out", err)
	}
	if st := sup.Status(); st.State != engine.Stopped || st.LastError == "" {
		t.Errorf("Status() = %+v, want stopped with the error", st)
	}
}

func TestAnEngineLeftByAPreviousRunIsReplaced(t *testing.T) {
	dir := t.TempDir()
	port := freePort(t)
	// A previous run created the credentials...
	first := newSupervisor(t, dir, port)
	if err := first.Start(context.Background()); err != nil {
		t.Fatalf("Start(): %v", err)
	}
	_ = first.Stop()
	// ...and crashed while its engine kept running.
	// #nosec G204 G702 -- the test binary itself, run as a fake TorrServer
	orphan := exec.Command(os.Args[0], "--ip", "127.0.0.1", "--port", strconv.Itoa(port), "--path", dir, "--httpauth")
	orphan.Env = append(os.Environ(), enginetest.FakeEnv+"=1")
	if err := orphan.Start(); err != nil {
		t.Fatalf("start orphan: %v", err)
	}
	exited := make(chan struct{})
	go func() { _ = orphan.Wait(); close(exited) }()
	t.Cleanup(func() { _ = orphan.Process.Kill() })
	for get(t, "http://127.0.0.1:"+strconv.Itoa(port), "", "") == 0 {
		time.Sleep(10 * time.Millisecond)
	}

	sup := newSupervisor(t, dir, port)
	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start() with an orphaned engine on the port: %v", err)
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("the orphaned engine is still running")
	}
	if st := sup.Status(); st.Port != port || st.State != engine.Running {
		t.Errorf("Status() = %+v, want running on port %d", st, port)
	}
}

func TestAPortTakenBySomethingElseIsLeftAlone(t *testing.T) {
	var shutdowns atomic.Int32
	// Someone's own TorrServer without --httpauth: accepts any credentials.
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/shutdown" {
			shutdowns.Add(1)
		}
		_, _ = w.Write([]byte("MatriX.145"))
	}))
	defer foreign.Close()
	taken, _ := strconv.Atoi(foreign.URL[strings.LastIndex(foreign.URL, ":")+1:])

	sup := newSupervisor(t, t.TempDir(), taken)
	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start(): %v", err)
	}
	if st := sup.Status(); st.Port == taken || st.State != engine.Running {
		t.Errorf("Status() = %+v, want running on another port than %d", st, taken)
	}
	if shutdowns.Load() != 0 {
		t.Error("the supervisor shut down a TorrServer it does not own")
	}
}

func TestAPortHeldOnEveryInterfaceIsLeftAlone(t *testing.T) {
	// Another TorrServer listening on every interface (*:port), as one
	// started without --ip does. macOS still lets 127.0.0.1:port be bound,
	// and then answers on loopback from the other one.
	ln, err := net.Listen("tcp", ":0") // #nosec G102 -- test: the other program listens on every interface
	if err != nil {
		t.Fatal(err)
	}
	foreign := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}), ReadHeaderTimeout: time.Second}
	go func() { _ = foreign.Serve(ln) }()
	t.Cleanup(func() { _ = foreign.Close() })
	taken := ln.Addr().(*net.TCPAddr).Port

	sup := newSupervisor(t, t.TempDir(), taken)
	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start(): %v", err)
	}
	if st := sup.Status(); st.Port == taken || st.State != engine.Running {
		t.Errorf("Status() = %+v, want running on another port than %d", st, taken)
	}
}

func TestAMissingBinaryIsReported(t *testing.T) {
	sup := engine.New(engine.Config{Binary: filepath.Join(t.TempDir(), "torrserver"), Dir: t.TempDir()})
	err := sup.Start(context.Background())
	var exitErr *exec.ExitError
	if err == nil || errors.As(err, &exitErr) {
		t.Fatalf("Start() error = %v, want a missing-binary error", err)
	}
	if st := sup.Status(); st.State != engine.Stopped || st.LastError == "" {
		t.Errorf("Status() = %+v, want stopped with the error", st)
	}
}

func engineArgs(t *testing.T, sup *engine.Supervisor) string {
	t.Helper()
	url, _, _ := sup.Endpoint()
	resp, err := http.Get(url + "/args")
	if err != nil {
		t.Fatalf("read engine args: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var args []string
	_ = json.NewDecoder(resp.Body).Decode(&args)
	return strings.Join(args, " ")
}

func TestStartupOptionsReachTheEngineAndChangeWithARestart(t *testing.T) {
	sup := engine.New(engine.Config{
		Binary:  os.Args[0],
		Dir:     t.TempDir(),
		Port:    freePort(t),
		Env:     []string{enginetest.FakeEnv + "=1"},
		Options: engine.Options{MaxSize: 50 << 30, ProxyURL: "socks5://10.0.0.2:1080", ProxyMode: "peers"},
	})
	t.Cleanup(func() { _ = sup.Stop() })
	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start(): %v", err)
	}
	args := engineArgs(t, sup)
	for _, want := range []string{"--maxsize 53687091200", "--proxyurl socks5://10.0.0.2:1080", "--proxymode peers"} {
		if !strings.Contains(args, want) {
			t.Errorf("engine args %q lack %q", args, want)
		}
	}
	if strings.Contains(args, "--pubipv4") || strings.Contains(args, "--torrentsdir") {
		t.Errorf("unset options were passed: %q", args)
	}

	if !sup.SetOptions(engine.Options{PublicIPv4: "203.0.113.7"}) {
		t.Fatal("SetOptions() with other options reported no change")
	}
	if err := sup.Restart(context.Background()); err != nil {
		t.Fatalf("Restart(): %v", err)
	}
	args = engineArgs(t, sup)
	if !strings.Contains(args, "--pubipv4 203.0.113.7") || strings.Contains(args, "--proxyurl") {
		t.Errorf("args after changing options = %q", args)
	}
	if sup.SetOptions(engine.Options{PublicIPv4: "203.0.113.7"}) {
		t.Error("SetOptions() with the same options reported a change")
	}
}

func TestAFreshEngineIsSetUpOnceOnItsFirstStart(t *testing.T) {
	dir, port := t.TempDir(), freePort(t)
	var calls atomic.Int32
	start := func() *engine.Supervisor {
		sup := engine.New(engine.Config{
			Binary: os.Args[0], Dir: dir, Port: port, Env: []string{enginetest.FakeEnv + "=1"},
			FirstStart: func(ctx context.Context, url, user, password string) error {
				calls.Add(1)
				if code := get(t, url, user, password); code != http.StatusOK {
					t.Errorf("FirstStart could not reach the engine: %d", code)
				}
				return nil
			},
		})
		if err := sup.Start(context.Background()); err != nil {
			t.Fatalf("Start(): %v", err)
		}
		return sup
	}
	first := start()
	_ = first.Restart(context.Background())
	_ = first.Stop()
	second := start()
	_ = second.Stop()
	if got := calls.Load(); got != 1 {
		t.Errorf("FirstStart ran %d times, want once for a fresh engine directory", got)
	}
}

func TestTheEngineStaysOnLoopbackAndServesHTTPSOnAPortOfItsOwn(t *testing.T) {
	port := freePort(t)
	sup := engine.New(engine.Config{
		Binary:  os.Args[0],
		Dir:     t.TempDir(),
		Port:    port,
		Env:     []string{enginetest.FakeEnv + "=1"},
		Options: engine.Options{HTTPS: true},
	})
	t.Cleanup(func() { _ = sup.Stop() })
	if err := sup.Start(context.Background()); err != nil {
		t.Fatalf("Start(): %v", err)
	}
	// 8091 is where other apps find HTTPS (the gateway); the engine's own
	// HTTPS stays on a loopback port nobody else uses.
	args := engineArgs(t, sup)
	if !strings.Contains(args, "--ssl") || !strings.Contains(args, "--ip 127.0.0.1") || !strings.Contains(args, "--sslport ") || strings.Contains(args, "--sslport 8091") {
		t.Errorf("HTTPS engine args = %q, want --ssl on loopback with a private --sslport", args)
	}

	sup.SetOptions(engine.Options{})
	if err := sup.Restart(context.Background()); err != nil {
		t.Fatalf("Restart(): %v", err)
	}
	if args := engineArgs(t, sup); strings.Contains(args, "--ssl") || !strings.Contains(args, "--ip 127.0.0.1") {
		t.Errorf("plain HTTP engine args = %q, want loopback without --ssl", args)
	}
}
