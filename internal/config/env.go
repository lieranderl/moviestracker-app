package config

import (
	"net/url"
	"strings"
)

// Env holds the environment variables that override stored sources, for
// development and scripted installs. A non-empty field is an override: the
// settings UI shows it read-only.
type Env struct {
	TMDBKey       string // TMDB_API_KEY
	JacRedURL     string // JACRED_URL
	JacRedAPIKey  string // JACRED_APIKEY
	IMDbURL       string // IMDB_SERVICE_URL
	TorrServerURL string // TORRSERVER_URL
	// TORRSERVER_USER and TORRSERVER_PASSWORD log in to it.
	TorrServerUser, TorrServerPassword string
	// SharedTMDBKey is the key built into release builds, used when there is
	// no other. It is never shown.
	SharedTMDBKey string
	// SharedJacRedKey is Moviestracker's jacred.su project key (unlimited
	// searches), built into release builds and used for jacred.su when
	// there is no other key. It is never shown, nor sent to another JacRed.
	SharedJacRedKey string
}

// UsesSharedJacRedKey reports whether JacRed searches run on the shared
// key: st's JacRed is jacred.su, with no JACRED_APIKEY and no key saved.
func (e Env) UsesSharedJacRedKey(st State) bool {
	url := st.Sources.JacRedURL
	if e.JacRedURL != "" {
		url = e.JacRedURL
	}
	return e.SharedJacRedKey != "" && e.JacRedAPIKey == "" && st.Sources.JacRedAPIKey == "" && IsJacredSu(url)
}

// IsJacredSu reports whether raw is the public jacred.su (or its search
// API's address), which the shared key is for.
func IsJacredSu(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && (u.Host == "jacred.su" || u.Host == "api.jacred.su")
}

// UsesSharedTMDBKey reports whether TMDB runs on the shared key: no
// TMDB_API_KEY and no key saved in st.
func (e Env) UsesSharedTMDBKey(st State) bool {
	return e.SharedTMDBKey != "" && e.TMDBKey == "" && st.Sources.TMDBKey == ""
}

// EnvFrom reads the overrides through getenv (os.Getenv in production).
func EnvFrom(getenv func(string) string) Env {
	get := func(name string) string { return strings.TrimSpace(getenv(name)) }
	return Env{
		TMDBKey:            get("TMDB_API_KEY"),
		JacRedURL:          get("JACRED_URL"),
		JacRedAPIKey:       get("JACRED_APIKEY"),
		IMDbURL:            get("IMDB_SERVICE_URL"),
		TorrServerURL:      get("TORRSERVER_URL"),
		TorrServerUser:     get("TORRSERVER_USER"),
		TorrServerPassword: getenv("TORRSERVER_PASSWORD"),
	}
}

// Apply returns st with the overrides in effect.
func (e Env) Apply(st State) State {
	sharedJacRed := e.UsesSharedJacRedKey(st)
	override := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	override(&st.Sources.TMDBKey, e.TMDBKey)
	override(&st.Sources.JacRedURL, e.JacRedURL)
	override(&st.Sources.JacRedAPIKey, e.JacRedAPIKey)
	override(&st.Sources.IMDbURL, e.IMDbURL)
	override(&st.TorrServer.URL, e.TorrServerURL)
	override(&st.TorrServer.User, e.TorrServerUser)
	override(&st.TorrServer.Password, e.TorrServerPassword)
	if st.Sources.TMDBKey == "" {
		st.Sources.TMDBKey = e.SharedTMDBKey
	}
	if sharedJacRed {
		st.Sources.JacRedAPIKey = e.SharedJacRedKey
	}
	return st
}
