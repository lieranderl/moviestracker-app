package config

import "strings"

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
	return st
}
