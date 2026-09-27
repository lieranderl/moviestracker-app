// Package config persists MoviesTracker's local state (accounts, sessions,
// sources and the TorrServer address) in its data directory.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// fileName is the state file inside the data directory.
const fileName = "moviestracker.json"

// Defaults for a fresh install.
const (
	DefaultJacRedURL     = "https://jacred.su" // #nosec G101 -- "JacRed" is a service name, not a credential
	DefaultIMDbURL       = "https://get-imdb-rating-service-asjvzhlb3q-ew.a.run.app"
	DefaultTorrServerURL = "http://127.0.0.1:8090"
)

// Roles a local account can have.
const (
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
)

// State is everything MoviesTracker keeps between runs.
type State struct {
	Users      []User     `json:"users"`
	Sessions   []Session  `json:"sessions"`
	Sources    Sources    `json:"sources"`
	TorrServer TorrServer `json:"torrserver"`
	// LinkSecret signs the stream links given to external players; replacing
	// it cancels every link.
	LinkSecret string `json:"linkSecret,omitempty"`
	// Titles are the movies and series TorrServer's torrents belong to, by
	// lowercase info hash, so the TorrServer page links to their pages.
	Titles map[string]TitleRef `json:"titles,omitempty"`
	// Gateway opens TorrServer to other apps (TorrServe, Lampa).
	Gateway Gateway `json:"gateway,omitzero"`
	// Updates is whether Moviestracker looks for new releases.
	Updates Updates `json:"updates,omitzero"`
}

// Updates is the administrator's choice about new releases: Moviestracker
// asks GitHub once a day unless Off.
type Updates struct {
	Off bool `json:"off,omitempty"`
}

// Gateway is TorrServer's door for other apps: off until an admin turns it
// on, and then for the home network unless Internet is on too.
type Gateway struct {
	Enabled  bool       `json:"enabled,omitempty"`
	Internet bool       `json:"internet,omitempty"`
	Logins   []AppLogin `json:"logins,omitempty"`
}

// AppLogin is the login one app uses. Only the bcrypt hash of its random
// password is kept.
type AppLogin struct {
	Name         string    `json:"name"`
	User         string    `json:"user"`
	PasswordHash string    `json:"passwordHash"`
	CreatedAt    time.Time `json:"createdAt"`
}

// TitleRef names a TMDB title: Kind is "movie" or "tv".
type TitleRef struct {
	Kind string `json:"kind"`
	ID   int    `json:"id"`
}

// User is a local account. Only the bcrypt hash of its password is kept.
type User struct {
	Username     string    `json:"username"`
	Name         string    `json:"name"`
	Role         string    `json:"role"`
	PasswordHash string    `json:"passwordHash"`
	CreatedAt    time.Time `json:"createdAt"`
}

// Session is a signed-in browser. Only a hash of its token is kept, so the
// state file cannot be used to take over a session.
type Session struct {
	TokenHash string    `json:"tokenHash"`
	Username  string    `json:"username"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// Sources are the external services discovery depends on.
type Sources struct {
	TMDBKey      string `json:"tmdbKey"`
	JacRedURL    string `json:"jacredUrl"`
	JacRedAPIKey string `json:"jacredApiKey"`
	IMDbURL      string `json:"imdbUrl"`
	IMDbOff      bool   `json:"imdbOff"`
}

// Engine modes: Moviestracker runs TorrServer itself, or uses one at URL.
const (
	EngineManaged  = "managed"
	EngineExternal = "external"
)

// TorrServer is the torrent engine: managed by Moviestracker, or an existing
// TorrServer at URL. An empty Mode means managed when a TorrServer program
// is available and external otherwise.
type TorrServer struct {
	Mode string `json:"mode,omitempty"`
	URL  string `json:"url"`
	// User and Password log in to an existing TorrServer started with
	// --httpauth. The password has to be sent, so it is kept as is; the
	// state file is readable by its owner only.
	User     string        `json:"user,omitempty"`
	Password string        `json:"password,omitempty"`
	Startup  EngineStartup `json:"startup,omitzero"`
}

// EngineStartup are the managed engine's command-line-only settings.
type EngineStartup struct {
	ProxyURL    string `json:"proxyUrl,omitempty"`
	ProxyMode   string `json:"proxyMode,omitempty"`
	PublicIPv4  string `json:"publicIPv4,omitempty"`
	PublicIPv6  string `json:"publicIPv6,omitempty"`
	MaxSize     int64  `json:"maxSize,omitempty"`
	TorrentsDir string `json:"torrentsDir,omitempty"`
}

// Store is the state file of one data directory. It is safe for concurrent use.
type Store struct {
	mu    sync.Mutex
	path  string
	state State
}

// Open loads the state of dir, creating the directory (owner-only) if needed.
// A missing state file is a fresh install.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	s := &Store{path: filepath.Join(dir, fileName), state: freshState()}
	raw, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, fmt.Errorf("read state: %w", err)
	}
	if err := json.Unmarshal(raw, &s.state); err != nil {
		return nil, fmt.Errorf("parse %s: %w", s.path, err)
	}
	return s, nil
}

func freshState() State {
	return State{
		Sources:    Sources{JacRedURL: DefaultJacRedURL, IMDbURL: DefaultIMDbURL},
		TorrServer: TorrServer{URL: DefaultTorrServerURL},
	}
}

// Dir is the data directory.
func (s *Store) Dir() string { return filepath.Dir(s.path) }

// State returns a copy of the current state.
func (s *Store) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.clone()
}

// Update applies fn to a copy of the state and saves the result. Nothing
// changes when fn or the save fails.
func (s *Store) Update(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.state.clone()
	if err := fn(&next); err != nil {
		return err
	}
	if err := s.write(next); err != nil {
		return err
	}
	s.state = next
	return nil
}

// write replaces the state file atomically: a crash leaves the old or the new
// file, never a torn one.
func (s *Store) write(st State) error {
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), fileName+".*.tmp")
	if err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("save state: %w", err)
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("save state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("save state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		return fmt.Errorf("save state: %w", err)
	}
	return nil
}

// clone copies st deeply enough that neither copy's slices and maps are
// the other's: State hands out snapshots that pages read while Update
// changes the next state.
func (st State) clone() State {
	c := st
	c.Users = append([]User(nil), st.Users...)
	c.Sessions = append([]Session(nil), st.Sessions...)
	c.Gateway.Logins = append([]AppLogin(nil), st.Gateway.Logins...)
	c.Titles = maps.Clone(st.Titles)
	return c
}
