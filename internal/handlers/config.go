package handlers

import (
	"errors"
	"fmt"
	playback "github.com/lieranderl/moviestracker-app/internal/streams"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/auth"
	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/engine"
	"github.com/lieranderl/moviestracker-app/internal/events"
	"github.com/lieranderl/moviestracker-app/internal/gateway"
	"github.com/lieranderl/moviestracker-app/internal/gstinstall"
	"github.com/lieranderl/moviestracker-app/internal/imdb"
	"github.com/lieranderl/moviestracker-app/internal/jacred"
	"github.com/lieranderl/moviestracker-app/internal/sources"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/update"
)

// Config defines bounded resources and security behavior for Server.
type Config struct {
	Sessions *auth.SessionManager
	// Accounts are the local user accounts; none means setup is pending.
	Accounts *auth.Accounts
	// Store keeps the sources and TorrServer address set in the UI.
	Store *config.Store
	// Env are the environment overrides; overridden settings are read-only.
	Env config.Env
	// Connector builds the TMDB/JacRed/IMDb clients when Sources change and
	// checks new sources before they are saved.
	Connector         sources.Connector
	SecureCookies     bool
	MaxSSEStreams     int
	LoginAttempts     int
	LoginWindow       time.Duration
	LoginClientKeys   int
	TrustedProxyCIDRs []string
	CatalogTimeout    time.Duration
	// TMDB, Details, Torrents and IMDb are the clients the server starts
	// with; saving Sources replaces them.
	TMDB       tmdb.CatalogProvider
	Details    tmdb.DetailsProvider
	Torrents   jacred.Searcher
	IMDb       imdb.RatingProvider
	TorrServer *torrserver.Manager
	// Engine runs TorrServer for managed mode; nil when no TorrServer
	// program is available.
	Engine *engine.Supervisor
	// GStreamer downloads GStreamer for the managed TorrServer (the macOS
	// app); nil where Moviestracker cannot.
	GStreamer *gstinstall.Installer
	// LivePolling sets how often the live topics poll TorrServer; zero
	// fields use the defaults.
	LivePolling LivePolling
	// StreamIdle is how long a playback session lasts after its player's
	// last request (30s when zero).
	StreamIdle time.Duration
	// Plays follows what plays, shared with the port for other apps so the
	// dashboard shows their streams too; nil makes one, with StreamIdle.
	Plays *playback.Tracker
	// Version is Moviestracker's version, shown on the dashboard.
	Version string
	// Updates tells whether a newer Moviestracker has been released; nil
	// never tells.
	Updates *update.Checker
	// AppsPort is the gateway's port, where other apps (TorrServe, Lampa)
	// reach TorrServer once an admin turns it on; nil hides Other apps.
	AppsPort *gateway.Port
	// LANAddress returns this machine's address on the local network, used in
	// links copied while browsing via localhost; nil finds it from the
	// default route.
	LANAddress func() string
	// SetupCode lets a browser on another device create the first admin
	// (the server prints it at startup); empty allows setup from this
	// machine only.
	SetupCode string
	// Events are the recent warnings and errors the dashboard shows; nil
	// shows none.
	Events *events.Recorder
}

// LivePolling is how often each live topic polls TorrServer while a page
// shows it. One poll serves every open page.
type LivePolling struct {
	Engine   time.Duration // version and GStreamer support (default 5s)
	Torrents time.Duration // the torrent list (default 2s)
	Player   time.Duration // a playing torrent (default 1s)
}

func (p LivePolling) withDefaults() LivePolling {
	if p.Engine <= 0 {
		p.Engine = 5 * time.Second
	}
	if p.Torrents <= 0 {
		p.Torrents = 2 * time.Second
	}
	if p.Player <= 0 {
		p.Player = time.Second
	}
	return p
}

// Validate rejects configurations that would create unbounded or unusable resources.
func (c Config) Validate() error {
	if c.Sessions == nil {
		return errors.New("sessions must not be nil")
	}
	if c.Accounts == nil || c.Store == nil {
		return errors.New("accounts and store must not be nil")
	}
	if c.MaxSSEStreams <= 0 {
		return fmt.Errorf("max SSE streams must be positive: %d", c.MaxSSEStreams)
	}
	if c.LoginAttempts <= 0 {
		return fmt.Errorf("login attempts must be positive: %d", c.LoginAttempts)
	}
	if c.LoginWindow <= 0 {
		return fmt.Errorf("login window must be positive: %s", c.LoginWindow)
	}
	if c.LoginClientKeys <= 0 {
		return fmt.Errorf("login client keys must be positive: %d", c.LoginClientKeys)
	}
	if c.CatalogTimeout <= 0 {
		return fmt.Errorf("catalog timeout must be positive: %s", c.CatalogTimeout)
	}
	return nil
}
