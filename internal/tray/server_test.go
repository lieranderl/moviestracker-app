package tray_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/tray"
)

// fakeEnv makes the test binary a fake moviestracker-server: it serves
// /healthz on MT_LISTEN, reports its environment at /env, stops when its
// input closes (MT_STOP_WITH_STDIN=1) and crashes after FAKE_EXIT_AFTER.
const fakeEnv = "TRAY_FAKE_SERVER"

// holderEnv makes the test binary a stand-in for the tray app: it runs the
// server with the data directory and port given ("dir|port") until killed.
const holderEnv = "TRAY_TEST_HOLDER"

func TestMain(m *testing.M) {
	switch {
	case os.Getenv(fakeEnv) == "1":
		fakeServer()
	case os.Getenv(holderEnv) != "":
		holdServer()
	}
	os.Exit(m.Run())
}

func fakeServer() {
	fmt.Println("fake server starting")
	if os.Getenv("MT_STOP_WITH_STDIN") == "1" {
		go func() {
			_, _ = io.Copy(io.Discard, os.Stdin)
			os.Exit(0)
		}()
	}
	if after, err := time.ParseDuration(os.Getenv("FAKE_EXIT_AFTER")); err == nil {
		time.AfterFunc(after, func() { os.Exit(3) })
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc("GET /env", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"MT_LISTEN": os.Getenv("MT_LISTEN"), "MT_DATA_DIR": os.Getenv("MT_DATA_DIR"),
			"MT_STOP_WITH_STDIN": os.Getenv("MT_STOP_WITH_STDIN"),
		})
	})
	_, port, _ := net.SplitHostPort(os.Getenv("MT_LISTEN"))
	srv := &http.Server{Addr: "127.0.0.1:" + port, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	fmt.Fprintln(os.Stderr, "fake server:", srv.ListenAndServe())
	os.Exit(2)
}

func holdServer() {
	dir, port, _ := strings.Cut(os.Getenv(holderEnv), "|")
	n, _ := strconv.Atoi(port)
	s := tray.NewServer(tray.Config{Program: os.Args[0], DataDir: dir, Port: n, Env: []string{fakeEnv + "=1", holderEnv + "="}})
	s.Start()
	select {}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

func newServer(t *testing.T, dir string, port int, env ...string) *tray.Server {
	t.Helper()
	s := tray.NewServer(tray.Config{
		Program:     os.Args[0],
		DataDir:     dir,
		Port:        port,
		Env:         append([]string{fakeEnv + "=1"}, env...),
		HealthEvery: 50 * time.Millisecond,
		MinBackoff:  50 * time.Millisecond,
		MaxBackoff:  200 * time.Millisecond,
		StopTimeout: 5 * time.Second,
	})
	t.Cleanup(s.Stop)
	return s
}

// waitFor fails the test unless ok becomes true within 10 seconds.
func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting until %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func answers(port int) bool {
	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/healthz") // #nosec G107 -- loopback test server
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func TestTheTrayStartsTheServerAndStopsIt(t *testing.T) {
	dir, port := t.TempDir(), freePort(t)
	s := newServer(t, dir, port)
	if got := s.Status().State; got != tray.Stopped {
		t.Fatalf("a new tray server is %q, want stopped", got)
	}
	s.Start()
	waitFor(t, "the server runs", func() bool { return s.Status().State == tray.Running })

	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/env") // #nosec G107 -- loopback test server
	if err != nil {
		t.Fatal(err)
	}
	var env map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&env)
	_ = resp.Body.Close()
	want := map[string]string{"MT_LISTEN": ":" + strconv.Itoa(port), "MT_DATA_DIR": dir, "MT_STOP_WITH_STDIN": "1"}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("the server got %s=%q, want %q", k, env[k], v)
		}
	}

	started := time.Now()
	s.Stop()
	if took := time.Since(started); took > 3*time.Second {
		t.Errorf("Stop() took %s; closing its input should stop the server at once", took)
	}
	if answers(port) {
		t.Error("the server still answers after Stop")
	}
	if got := s.Status(); got.State != tray.Stopped {
		t.Errorf("after Stop the tray says %+v, want stopped", got)
	}
	log, err := os.ReadFile(tray.LogPath(dir)) // #nosec G304 -- the test's own directory
	if err != nil || !strings.Contains(string(log), "fake server starting") {
		t.Errorf("the log lacks the server's output (%v):\n%s", err, log)
	}
}

func TestAnotherMoviestrackerOnThePortIsLeftAloneUntilItStops(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	other := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})}
	go func() { _ = other.Serve(ln) }()

	s := newServer(t, t.TempDir(), port)
	s.Start()
	waitFor(t, "the tray sees the other Moviestracker", func() bool { return s.Status().State == tray.OtherInstance })

	_ = other.Close()
	waitFor(t, "the tray starts its own server", func() bool { return s.Status().State == tray.Running })
}

func TestACrashedServerIsStartedAgain(t *testing.T) {
	s := newServer(t, t.TempDir(), freePort(t), "FAKE_EXIT_AFTER=300ms")
	s.Start()
	waitFor(t, "the server runs", func() bool { return s.Status().State == tray.Running })
	waitFor(t, "the tray reports the crash", func() bool {
		st := s.Status()
		return st.State == tray.Stopped && strings.Contains(st.Reason, "starting again")
	})
	waitFor(t, "the server runs again", func() bool { return s.Status().State == tray.Running })
}

func TestTheServerStopsWhenTheTrayIsKilled(t *testing.T) {
	port := freePort(t)
	holder := exec.Command(os.Args[0], "-test.run=^$") // #nosec G204 G702 -- this test binary
	holder.Env = append(os.Environ(), holderEnv+"="+t.TempDir()+"|"+strconv.Itoa(port))
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = holder.Process.Kill() })
	waitFor(t, "the server runs", func() bool { return answers(port) })

	_ = holder.Process.Kill()
	var exit *exec.ExitError
	if err := holder.Wait(); err != nil && !errors.As(err, &exit) {
		t.Fatal(err)
	}
	waitFor(t, "the server stops", func() bool { return !answers(port) })
}

func TestTheLogIsCappedAtTenMegabytes(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("x", 11<<20)
	if err := os.WriteFile(tray.LogPath(dir), []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	tray.RotateLog(dir)
	if info, err := os.Stat(tray.LogPath(dir)); err != nil || info.Size() != 0 {
		t.Errorf("the log was not started afresh: %v", err)
	}
	if info, err := os.Stat(filepath.Join(dir, "moviestracker.log.1")); err != nil || info.Size() != int64(len(big)) {
		t.Errorf("the old log was not kept as moviestracker.log.1: %v", err)
	}
}
