package handlers

import (
	"crypto/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/lieranderl/moviestracker-app/internal/auth"
	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

// The account every validTestConfig server has: setup is complete.
const (
	testUsername = "alex"
	testPassword = "correct horse battery"
)

// testHashes caches low-cost bcrypt hashes: the real cost, run for every
// test server under the race detector, takes most of a minute.
var testHashes sync.Map // password -> []byte

// addTestAccount writes an account straight into store.
func addTestAccount(tb testing.TB, store *config.Store, username, name, role, password string) {
	tb.Helper()
	hash, ok := testHashes.Load(password)
	if !ok {
		h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
		if err != nil {
			tb.Fatalf("bcrypt: %v", err)
		}
		hash, _ = testHashes.LoadOrStore(password, h)
	}
	err := store.Update(func(st *config.State) error {
		st.Users = append(st.Users, config.User{Username: username, Name: name, Role: role, PasswordHash: string(hash.([]byte))})
		return nil
	})
	if err != nil {
		tb.Fatalf("add account: %v", err)
	}
}

func validTestConfig(tb testing.TB) Config {
	tb.Helper()
	store, err := config.Open(tb.TempDir())
	if err != nil {
		tb.Fatalf("config.Open(): %v", err)
	}
	addTestAccount(tb, store, testUsername, "Alex Rivers", config.RoleAdmin, testPassword)
	return Config{
		Accounts:        auth.NewAccounts(store),
		Store:           store,
		Sessions:        auth.NewSessionManager(32, rand.Reader),
		SecureCookies:   false,
		MaxSSEStreams:   1,
		LoginAttempts:   2,
		LoginWindow:     time.Minute,
		LoginClientKeys: 2,
		CatalogTimeout:  time.Second,
		// Nothing listens here, so tests never reach a real TorrServer.
		TorrServer: torrserver.NewManager("http://127.0.0.1:1"),
	}
}

func TestNewServerRejectsInvalidTrustedProxyCIDR(t *testing.T) {
	cfg := validTestConfig(t)
	defer cfg.Sessions.Close()
	cfg.TrustedProxyCIDRs = []string{"not-a-cidr"}

	_, err := NewServer(cfg)
	if err == nil {
		t.Fatal("NewServer() error = nil, want invalid trusted proxy error")
	}
	if !strings.Contains(err.Error(), "not-a-cidr") {
		t.Fatalf("NewServer() error = %q, want offending CIDR", err)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "nil sessions", mutate: func(c *Config) { c.Sessions = nil }},
		{name: "zero SSE streams", mutate: func(c *Config) { c.MaxSSEStreams = 0 }},
		{name: "zero login attempts", mutate: func(c *Config) { c.LoginAttempts = 0 }},
		{name: "zero login window", mutate: func(c *Config) { c.LoginWindow = 0 }},
		{name: "zero client keys", mutate: func(c *Config) { c.LoginClientKeys = 0 }},
		{name: "zero catalog timeout", mutate: func(c *Config) { c.CatalogTimeout = 0 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validTestConfig(t)
			defer cfg.Sessions.Close()
			tt.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
			if _, err := NewServer(cfg); err == nil {
				t.Fatal("NewServer() error = nil, want error")
			}
		})
	}
}

func TestLoginLimiterAttemptWindow(t *testing.T) {
	limiter := newLoginLimiter(4, 2, time.Minute)
	now := time.Unix(1_000, 0)

	if !limiter.Allow("client", now) || !limiter.Allow("client", now.Add(time.Second)) {
		t.Fatal("first two attempts should be allowed")
	}
	if limiter.Allow("client", now.Add(2*time.Second)) {
		t.Fatal("third attempt inside window should be denied")
	}
	if !limiter.Allow("client", now.Add(time.Minute)) {
		t.Fatal("attempt at next window should be allowed")
	}
}

func TestLoginLimiterBoundsClientKeys(t *testing.T) {
	limiter := newLoginLimiter(2, 1, time.Minute)
	now := time.Unix(1_000, 0)

	if !limiter.Allow("one", now) || !limiter.Allow("two", now) {
		t.Fatal("first two client keys should be allowed")
	}
	if limiter.Allow("three", now) {
		t.Fatal("new client key should be denied at capacity")
	}
	if !limiter.Allow("three", now.Add(time.Minute)) {
		t.Fatal("expired keys should be pruned before capacity check")
	}
	if !limiter.Allow("four", now.Add(time.Minute)) {
		t.Fatal("second slot after prune should be allowed")
	}
	if limiter.Allow("five", now.Add(time.Minute)) {
		t.Fatal("key beyond capacity after prune should be denied")
	}
}

func TestServerSSEAdmissionAndClose(t *testing.T) {
	server, err := NewServer(validTestConfig(t))
	if err != nil {
		t.Fatalf("NewServer(): %v", err)
	}

	if !server.acquireSSE() {
		t.Fatal("first SSE stream should be admitted")
	}
	if server.acquireSSE() {
		t.Fatal("second SSE stream should be rejected at capacity")
	}
	server.releaseSSE()
	if !server.acquireSSE() {
		t.Fatal("SSE permit should be reusable after release")
	}
	server.releaseSSE()

	var wg sync.WaitGroup
	for range 32 {
		wg.Go(server.Close)
	}
	wg.Wait()
}
