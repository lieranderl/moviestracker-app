package auth_test

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/auth"
	"github.com/lieranderl/moviestracker-app/internal/config"
)

func openStore(t *testing.T, dir string) *config.Store {
	t.Helper()
	store, err := config.Open(dir)
	if err != nil {
		t.Fatalf("config.Open(): %v", err)
	}
	return store
}

func TestTheFirstAdminIsCreatedOnceAndCanSignIn(t *testing.T) {
	accounts := auth.NewAccounts(openStore(t, t.TempDir()))
	if !accounts.NeedsSetup() {
		t.Fatal("NeedsSetup() = false on a fresh install")
	}

	admin, err := accounts.CreateFirstAdmin("Evgenii", "  Evgenii ", "correct horse battery")
	if err != nil {
		t.Fatalf("CreateFirstAdmin(): %v", err)
	}
	if admin.Username != "evgenii" || admin.Name != "Evgenii" || !admin.IsAdmin() {
		t.Errorf("admin = %+v, want username evgenii, name Evgenii, admin role", admin)
	}
	if accounts.NeedsSetup() {
		t.Error("NeedsSetup() = true after the admin exists")
	}
	if _, err := accounts.CreateFirstAdmin("intruder", "Intruder", "another long password"); !errors.Is(err, auth.ErrSetupDone) {
		t.Errorf("second CreateFirstAdmin() error = %v, want ErrSetupDone", err)
	}

	user, err := accounts.Authenticate("EVGENII", "correct horse battery")
	if err != nil || user.Username != "evgenii" {
		t.Fatalf("Authenticate(right password) = %+v, %v", user, err)
	}
	if _, err := accounts.Authenticate("evgenii", "wrong password"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("Authenticate(wrong password) error = %v, want ErrInvalidCredentials", err)
	}
	if _, err := accounts.Authenticate("nobody", "correct horse battery"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Errorf("Authenticate(unknown user) error = %v, want ErrInvalidCredentials", err)
	}
}

func TestAccountsSurviveARestart(t *testing.T) {
	dir := t.TempDir()
	if _, err := auth.NewAccounts(openStore(t, dir)).CreateFirstAdmin("admin", "Admin", "correct horse battery"); err != nil {
		t.Fatalf("CreateFirstAdmin(): %v", err)
	}
	restarted := auth.NewAccounts(openStore(t, dir))
	if restarted.NeedsSetup() {
		t.Fatal("setup is offered again after a restart")
	}
	if _, err := restarted.Authenticate("admin", "correct horse battery"); err != nil {
		t.Fatalf("Authenticate() after restart: %v", err)
	}
}

func TestSetupRefusesUnusableAccounts(t *testing.T) {
	accounts := auth.NewAccounts(openStore(t, t.TempDir()))
	for _, tc := range []struct{ username, password string }{
		{"", "correct horse battery"},
		{"has space", "correct horse battery"},
		{"admin", "short"},
	} {
		if _, err := accounts.CreateFirstAdmin(tc.username, "Name", tc.password); !errors.Is(err, auth.ErrInvalidAccount) {
			t.Errorf("CreateFirstAdmin(%q, %q) error = %v, want ErrInvalidAccount", tc.username, tc.password, err)
		}
	}
	if !accounts.NeedsSetup() {
		t.Error("a refused account was created")
	}
}

func TestASignedInBrowserStaysSignedInAcrossARestartUntilItExpires(t *testing.T) {
	dir := t.TempDir()
	store := openStore(t, dir)
	accounts := auth.NewAccounts(store)
	admin, err := accounts.CreateFirstAdmin("admin", "Admin", "correct horse battery")
	if err != nil {
		t.Fatalf("CreateFirstAdmin(): %v", err)
	}
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }

	first := auth.NewSessionManager(16, rand.Reader, auth.WithClock(clock), auth.WithStore(store, accounts.Lookup))
	token, err := first.CreateSession(admin)
	if err != nil {
		t.Fatalf("CreateSession(): %v", err)
	}
	first.Close()

	raw, err := os.ReadFile(filepath.Join(dir, "moviestracker.json")) // #nosec G304 -- the test's own temp dir
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if strings.Contains(string(raw), token) {
		t.Error("the state file contains the raw session token")
	}

	restartedStore := openStore(t, dir)
	restarted := auth.NewSessionManager(16, rand.Reader, auth.WithClock(clock),
		auth.WithStore(restartedStore, auth.NewAccounts(restartedStore).Lookup))
	defer restarted.Close()
	if user := restarted.GetUser(token); user == nil || user.Username != "admin" {
		t.Fatalf("GetUser() after restart = %+v, want admin", user)
	}

	now = now.Add(8 * 24 * time.Hour)
	if user := restarted.GetUser(token); user != nil {
		t.Errorf("GetUser() after 8 days = %+v, want nil: sessions last 7 days", user)
	}
}
