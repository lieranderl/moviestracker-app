package main

import (
	"crypto/rand"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/engine"
)

func TestEnvBool(t *testing.T) {
	t.Run("missing value uses secure default", func(t *testing.T) {
		if !envBool("DATASTAR_TEST_MISSING_BOOL", true) {
			t.Fatal("missing value did not use true default")
		}
	})

	t.Run("explicit false", func(t *testing.T) {
		t.Setenv("DATASTAR_TEST_BOOL", "false")
		if envBool("DATASTAR_TEST_BOOL", true) {
			t.Fatal("explicit false parsed as true")
		}
	})

	t.Run("invalid value uses secure default", func(t *testing.T) {
		t.Setenv("DATASTAR_TEST_BOOL", "definitely-not-a-bool")
		if !envBool("DATASTAR_TEST_BOOL", true) {
			t.Fatal("invalid value did not fail closed to true")
		}
	})
}

func TestEnvList(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{name: "empty"},
		{name: "whitespace", value: "   "},
		{name: "two values", value: "10.0.0.0/8, 192.0.2.0/24", want: []string{"10.0.0.0/8", "192.0.2.0/24"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DATASTAR_TEST_LIST", tt.value)
			if got := envList("DATASTAR_TEST_LIST"); !slices.Equal(got, tt.want) {
				t.Fatalf("envList() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAnInstallThatAlreadyHasAccountsKeepsItsTorrServer(t *testing.T) {
	sup := engine.New(engine.Config{Binary: "/bin/sh", Dir: t.TempDir()}) // a TorrServer program is available
	for _, tc := range []struct {
		name     string
		accounts []config.User
		want     string
	}{
		{"fresh install", nil, config.EngineManaged},
		{"install from before engine modes", []config.User{{Username: "test"}}, config.EngineExternal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := config.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Update(func(st *config.State) error { st.Users = tc.accounts; return nil }); err != nil {
				t.Fatal(err)
			}
			if err := settleEngineMode(store, sup); err != nil {
				t.Fatal(err)
			}
			if got := store.State().TorrServer.Mode; got != tc.want {
				t.Errorf("mode = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestTheServerNoticesWhenTheAppThatStartedItIsGone(t *testing.T) {
	select {
	case <-parentGone(os.Getppid(), 10*time.Millisecond):
		t.Fatal("parent reported gone while it is alive")
	case <-time.After(100 * time.Millisecond):
	}
	select {
	case <-parentGone(os.Getppid()+1_000_000, 10*time.Millisecond):
	case <-time.After(time.Second):
		t.Fatal("a launcher that is no longer the parent was not noticed")
	}
}

// The Windows tray app holds the server's standard input open: when the app
// quits, or is killed, the input closes and the server stops.
func TestTheServerNoticesWhenTheAppClosesItsInput(t *testing.T) {
	r, w := io.Pipe()
	closed := inputClosed(r)
	if _, err := w.Write([]byte("anything\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
		t.Fatal("reported closed while the app holds the input open")
	case <-time.After(100 * time.Millisecond):
	}
	_ = w.Close()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("the closed input was not noticed")
	}
}

// The container's health check runs `moviestracker --health`: the image has
// no curl.
func TestTheHealthCheckAsksTheRunningServer(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer up.Close()
	_, port, _ := net.SplitHostPort(up.Listener.Addr().String())
	for _, listen := range []string{":" + port, "0.0.0.0:" + port, "127.0.0.1:" + port} {
		if err := checkHealth(listen); err != nil {
			t.Errorf("checkHealth(%q) with the server up: %v", listen, err)
		}
	}

	up.Close()
	if err := checkHealth(":" + port); err == nil {
		t.Error("checkHealth reports healthy with nothing listening")
	}
	sick := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "starting", http.StatusServiceUnavailable)
	}))
	defer sick.Close()
	_, port, _ = net.SplitHostPort(sick.Listener.Addr().String())
	if err := checkHealth(":" + port); err == nil {
		t.Error("checkHealth reports healthy for a failing /healthz")
	}
}

// Links for TVs replace "localhost" with this machine's network address. In a
// container that is the container's own address, so the image turns the
// guess off and MT_LAN_ADDRESS can name the host's address instead.
func TestTheAddressForOtherDevicesCanBeSetOrTurnedOff(t *testing.T) {
	if lanAddressFrom("") != nil {
		t.Error("an empty MT_LAN_ADDRESS should keep the automatic address")
	}
	if got := lanAddressFrom("off")(); got != "" {
		t.Errorf("MT_LAN_ADDRESS=off gives %q, want no address", got)
	}
	if got := lanAddressFrom(" 192.168.1.20 ")(); got != "192.168.1.20" {
		t.Errorf("MT_LAN_ADDRESS=192.168.1.20 gives %q", got)
	}
}

// A NAS folder mounted at /data that the container's user cannot write to
// is the usual first-run mistake: the error names the user and the fix.
func TestADataDirectoryItCannotWriteIsExplained(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("needs Unix permissions and a user they apply to")
	}
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) // #nosec G302 -- lets the test clean up
	err := checkWritable(dir)
	if err == nil {
		t.Fatal("checkWritable accepts a read-only directory")
	}
	for _, want := range []string{dir, "uid " + strconv.Itoa(os.Getuid()), "chown"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the error %q does not mention %q", err, want)
		}
	}
	if err := checkWritable(t.TempDir()); err != nil {
		t.Errorf("checkWritable refuses a writable directory: %v", err)
	}
}

func TestTheSetupCodeIsEightUnambiguousCharactersFromEntropy(t *testing.T) {
	seen := map[string]bool{}
	for range 20 {
		code, err := newSetupCode(rand.Reader)
		if err != nil {
			t.Fatalf("newSetupCode(): %v", err)
		}
		if !regexp.MustCompile(`^[23456789ABCDEFGHJKMNPQRSTUVWXYZ]{4}-[23456789ABCDEFGHJKMNPQRSTUVWXYZ]{4}$`).MatchString(code) {
			t.Fatalf("setup code %q is not XXXX-XXXX without 0/O, 1/I/L", code)
		}
		seen[code] = true
	}
	if len(seen) < 20 {
		t.Errorf("20 setup codes had only %d distinct values", len(seen))
	}
	if _, err := newSetupCode(strings.NewReader("short")); err == nil {
		t.Error("a failing entropy source still gave a setup code")
	}
}
