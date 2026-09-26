package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/config"
)

func TestSettingsSavedByOneRunAreReadByTheNext(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	store, err := config.Open(dir)
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}
	err = store.Update(func(s *config.State) error {
		s.Sources.TMDBKey = "tmdb-secret"
		s.TorrServer.URL = "http://192.168.1.50:8090"
		return nil
	})
	if err != nil {
		t.Fatalf("Update(): %v", err)
	}

	reopened, err := config.Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	got := reopened.State()
	if got.Sources.TMDBKey != "tmdb-secret" || got.TorrServer.URL != "http://192.168.1.50:8090" {
		t.Fatalf("reopened state = %+v, want the saved TMDB key and TorrServer URL", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, "moviestracker.json"))
		if err != nil {
			t.Fatalf("stat state file: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("state file mode = %o, want 600: it holds keys and password hashes", perm)
		}
	}
}

func TestAFreshInstallStartsFromDefaults(t *testing.T) {
	store, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}
	got := store.State()
	if len(got.Users) != 0 {
		t.Errorf("fresh install has users %+v", got.Users)
	}
	if got.Sources.JacRedURL != "https://jacred.su" || got.Sources.TMDBKey != "" {
		t.Errorf("fresh sources = %+v, want public JacRed and no TMDB key", got.Sources)
	}
	if got.TorrServer.URL != "http://127.0.0.1:8090" {
		t.Errorf("fresh TorrServer URL = %q, want http://127.0.0.1:8090", got.TorrServer.URL)
	}
}

func TestARejectedUpdateChangesNothing(t *testing.T) {
	dir := t.TempDir()
	store, err := config.Open(dir)
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}
	refused := errors.New("refused")
	err = store.Update(func(s *config.State) error {
		s.Sources.TMDBKey = "half-written"
		return refused
	})
	if !errors.Is(err, refused) {
		t.Fatalf("Update() error = %v, want the callback's error", err)
	}
	if got := store.State().Sources.TMDBKey; got != "" {
		t.Errorf("TMDB key after refused update = %q, want empty", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "moviestracker.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("state file written by a refused update (stat error %v)", err)
	}
}

func TestEnvironmentVariablesOverrideStoredSources(t *testing.T) {
	vars := map[string]string{ // #nosec G101 -- test fixtures, not credentials
		"TMDB_API_KEY":   "dev-key",
		"JACRED_URL":     "http://jacred.lan:9117",
		"TORRSERVER_URL": "http://nas.lan:8090",
	}
	env := config.EnvFrom(func(name string) string { return vars[name] })
	stored := config.State{
		Sources:    config.Sources{TMDBKey: "stored-key", JacRedURL: "https://jacred.su", JacRedAPIKey: "stored-jacred", IMDbURL: "https://imdb.example"}, // #nosec G101 -- test fixtures
		TorrServer: config.TorrServer{URL: "http://127.0.0.1:8090"},
	}

	got := env.Apply(stored)
	want := config.State{
		Sources:    config.Sources{TMDBKey: "dev-key", JacRedURL: "http://jacred.lan:9117", JacRedAPIKey: "stored-jacred", IMDbURL: "https://imdb.example"}, // #nosec G101 -- test fixtures
		TorrServer: config.TorrServer{URL: "http://nas.lan:8090"},
	}
	if got.Sources != want.Sources || got.TorrServer != want.TorrServer {
		t.Errorf("Apply() = %+v %+v\nwant %+v %+v", got.Sources, got.TorrServer, want.Sources, want.TorrServer)
	}
	if env.TMDBKey == "" || env.JacRedAPIKey != "" {
		t.Errorf("env = %+v: set variables must show as overrides, unset ones must not", env)
	}
}

func TestTheSharedTMDBKeyIsUsedOnlyWithoutAnOwnKey(t *testing.T) {
	for _, tc := range []struct {
		name, env, stored, want string
		shared                  bool
	}{
		{"nothing set", "", "", "shared-key", true},
		{"a key saved in Sources", "", "own-key", "own-key", false},
		{"TMDB_API_KEY", "dev-key", "own-key", "dev-key", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := config.EnvFrom(func(name string) string {
				if name == "TMDB_API_KEY" {
					return tc.env
				}
				return ""
			})
			env.SharedTMDBKey = "shared-key" // #nosec G101 -- a test fixture
			st := config.State{Sources: config.Sources{TMDBKey: tc.stored}}
			if got := env.Apply(st).Sources.TMDBKey; got != tc.want {
				t.Errorf("TMDB key in use = %q, want %q", got, tc.want)
			}
			if got := env.UsesSharedTMDBKey(st); got != tc.shared {
				t.Errorf("UsesSharedTMDBKey() = %v, want %v", got, tc.shared)
			}
		})
	}
}

// Moviestracker's JacRed project key is used for jacred.su only, and only
// without a key of one's own: it never goes to another JacRed.
func TestTheSharedJacRedKeyIsUsedOnlyForJacredSuWithoutAnOwnKey(t *testing.T) {
	for _, tc := range []struct {
		name, env, url, stored, want string
		shared                       bool
	}{
		{"nothing set", "", "https://jacred.su", "", "project-key", true},
		{"the search API's address", "", "https://api.jacred.su/", "", "project-key", true},
		{"a key saved in Sources", "", "https://jacred.su", "own-key", "own-key", false},
		{"JACRED_APIKEY", "dev-key", "https://jacred.su", "own-key", "dev-key", false},
		{"a private JacRed", "", "http://nas:9117", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := config.EnvFrom(func(name string) string {
				if name == "JACRED_APIKEY" {
					return tc.env
				}
				return ""
			})
			env.SharedJacRedKey = "project-key" // #nosec G101 -- a test fixture
			st := config.State{Sources: config.Sources{JacRedURL: tc.url, JacRedAPIKey: tc.stored}}
			if got := env.Apply(st).Sources.JacRedAPIKey; got != tc.want {
				t.Errorf("JacRed key in use = %q, want %q", got, tc.want)
			}
			if got := env.UsesSharedJacRedKey(st); got != tc.shared {
				t.Errorf("UsesSharedJacRedKey() = %v, want %v", got, tc.shared)
			}
		})
	}
}
