package handlers

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/auth"
	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/engine"
	"github.com/lieranderl/moviestracker-app/internal/events"
	"github.com/lieranderl/moviestracker-app/internal/gateway"
	"github.com/lieranderl/moviestracker-app/internal/gstinstall"
	"github.com/lieranderl/moviestracker-app/internal/live"
	"github.com/lieranderl/moviestracker-app/internal/sources"
	"github.com/lieranderl/moviestracker-app/internal/stats"
	"github.com/lieranderl/moviestracker-app/internal/streamlink"
	playback "github.com/lieranderl/moviestracker-app/internal/streams"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"
	webstatic "github.com/lieranderl/moviestracker-app/static"
)

// Server holds application state and dependencies for HTTP handling.
type Server struct {
	mux             *http.ServeMux
	handler         http.Handler // mux behind the setup gate and cross-origin (CSRF) protection
	sessions        *auth.SessionManager
	lanAddress      func() string
	presence        *presence
	events          *events.Recorder
	accounts        *auth.Accounts
	store           *config.Store
	env             config.Env
	connector       sources.Connector
	current         atomic.Pointer[sources.Clients] // replaced when Sources are saved
	secureCookies   bool
	loginLimiter    *loginLimiter
	loginRetryAfter string
	trustedProxies  []netip.Prefix
	sseSlots        chan struct{}
	streams         context.Context // canceled by Close to end every SSE stream
	stopStreams     context.CancelFunc
	closeOnce       sync.Once
	catalogTimeout  time.Duration
	torrServer      *torrserver.Manager
	engine          *engine.Supervisor                // nil without a TorrServer program
	gst             *gstinstall.Installer             // downloads GStreamer in the macOS app; nil elsewhere
	links           atomic.Pointer[streamlink.Signer] // signs the links external players open
	playing         atomic.Int64                      // media responses streaming through the proxy
	live            *live.Hub                         // shared pollers behind every live view
	plays           *playback.Tracker                 // what plays through the proxy
	sampler         *stats.Sampler                    // this machine, for the dashboard
	startedAt       time.Time
	version         string
	hlsOutputs      hlsOutputCache         // what GStreamer serves, as seen by the stream proxy
	setupCode       string                 // lets another device create the first admin
	titles          titleLookups           // torrents whose titles were looked up on TMDB
	appsPort        *gateway.Port          // other apps' door to TorrServer; nil without one
	appsProblem     atomic.Pointer[string] // why the apps port did not open at startup
}

// NewServer initializes all HTTP routes and returns the configured Server.
func NewServer(cfg Config) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate server config: %w", err)
	}
	trustedProxies, err := parseTrustedProxyCIDRs(cfg.TrustedProxyCIDRs)
	if err != nil {
		return nil, fmt.Errorf("parse trusted proxy CIDRs: %w", err)
	}
	retrySeconds := max(1, int((cfg.LoginWindow+time.Second-1)/time.Second))
	torrMgr := cfg.TorrServer
	if torrMgr == nil {
		torrMgr = torrserver.NewManagerFor(cfg.Env.Apply(cfg.Store.State()).TorrServer)
	}
	streams, stopStreams := context.WithCancel(context.Background())
	s := &Server{
		mux:             http.NewServeMux(),
		sessions:        cfg.Sessions,
		lanAddress:      cfg.LANAddress,
		events:          cfg.Events,
		accounts:        cfg.Accounts,
		store:           cfg.Store,
		env:             cfg.Env,
		connector:       cfg.Connector,
		secureCookies:   cfg.SecureCookies,
		loginLimiter:    newLoginLimiter(cfg.LoginClientKeys, cfg.LoginAttempts, cfg.LoginWindow),
		loginRetryAfter: strconv.Itoa(retrySeconds),
		trustedProxies:  trustedProxies,
		sseSlots:        make(chan struct{}, cfg.MaxSSEStreams),
		streams:         streams,
		stopStreams:     stopStreams,
		catalogTimeout:  cfg.CatalogTimeout,
		torrServer:      torrMgr,
		engine:          cfg.Engine,
		gst:             cfg.GStreamer,
		setupCode:       normalizeSetupCode(cfg.SetupCode),
		appsPort:        cfg.AppsPort,
	}
	s.openAppsPort()
	signer, err := linkSigner(cfg.Store)
	if err != nil {
		return nil, err
	}
	s.links.Store(signer)
	s.plays = playback.New(cmp.Or(cfg.StreamIdle, 30*time.Second))
	s.sampler = stats.NewSampler()
	s.startedAt, s.version = time.Now(), cmp.Or(cfg.Version, "dev")
	views.SetVersion(s.version)
	s.live = live.New(s.liveTopic(cfg.LivePolling.withDefaults()))
	s.current.Store(&sources.Clients{Catalog: cfg.TMDB, Details: cfg.Details, Torrents: cfg.Torrents, IMDb: cfg.IMDb})
	s.routes()
	// Rejects cross-origin POSTs (Sec-Fetch-Site / Origin), so no other site
	// can submit forms or Datastar actions with a visitor's cookies.
	s.presence = newPresence()
	s.handler = http.NewCrossOriginProtection().Handler(s.setupGate(s.presenceMiddleware(s.mux)))
	return s, nil
}

// clients returns the TMDB/JacRed/IMDb clients currently in use.
func (s *Server) clients() *sources.Clients { return s.current.Load() }

// Close ends open SSE streams and releases resources owned by the server.
// Register it with http.Server.RegisterOnShutdown: Shutdown alone waits for
// streams, which never go idle. It is safe to call concurrently.
func (s *Server) Close() {
	s.closeOnce.Do(func() {
		s.stopStreams()
		s.live.Close()
		s.sessions.Close()
	})
}

// streamContext bounds a long-lived SSE stream: it ends when the client goes
// away or the server closes.
func (s *Server) streamContext(r *http.Request) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(s.streams, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

func (s *Server) acquireSSE() bool { return tryAcquire(s.sseSlots) }

func (s *Server) releaseSSE() { <-s.sseSlots }

func tryAcquire(slots chan struct{}) bool {
	select {
	case slots <- struct{}{}:
		return true
	default:
		return false
	}
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

func (s *Server) routes() {
	// Health Endpoints
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /readyz", s.handleReadyz)

	// Static Assets
	fileServer := http.FileServer(http.FS(webstatic.Files))
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", fileServer))

	// Web Pages
	s.mux.HandleFunc("GET /{$}", s.handleRoot)
	s.mux.HandleFunc("GET /setup", s.handleSetupPage)
	s.mux.HandleFunc("GET /login", s.handleLoginPage)
	s.mux.HandleFunc("GET /settings", s.handleSettingsIndex)
	s.mux.HandleFunc("GET /settings/sources", s.handleSourcesPage)
	s.mux.HandleFunc("GET /settings/{section}", s.handleSettingsPage)
	s.mux.HandleFunc("GET /movies", s.handleMoviesPage)
	s.mux.HandleFunc("GET /dashboard", s.handleDashboardPage)
	s.mux.HandleFunc("GET /torrserver", s.handleTorrServerPage)
	s.mux.HandleFunc("GET /movie/{id}", s.handleMoviePage)
	s.mux.HandleFunc("GET /tv/{id}", s.handleTVPage)
	s.mux.HandleFunc("GET /person/{id}", s.handlePersonPage)
	s.mux.HandleFunc("GET /search", s.handleSearchPage)

	// API Endpoints
	s.mux.HandleFunc("POST /api/consent", s.handleConsent)
	s.mux.HandleFunc("POST /api/setup", s.handleSetup)
	s.mux.HandleFunc("POST /api/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/logout", s.handleLogout)
	s.mux.HandleFunc("POST /api/settings/sources/tmdb", s.handleSaveTMDB)
	s.mux.HandleFunc("POST /api/settings/sources/tmdb/shared", s.handleUseSharedTMDB)
	s.mux.HandleFunc("POST /api/settings/sources/jacred/shared", s.handleUseSharedJacRed)
	s.mux.HandleFunc("POST /api/settings/sources/jacred", s.handleSaveJacRed)
	s.mux.HandleFunc("POST /api/settings/sources/imdb", s.handleSaveIMDb)
	s.mux.HandleFunc("POST /api/settings/sources/torrserver", s.handleSaveTorrServer)
	s.mux.HandleFunc("POST /api/settings/engine/restart", s.handleRestartEngine)
	s.mux.HandleFunc("POST /api/settings/engine/{section}", s.handleSaveEngineSettings)
	s.mux.HandleFunc("POST /api/settings/gstreamer", s.handleSaveGStreamer)
	s.mux.HandleFunc("POST /api/settings/gstreamer/reset", s.handleResetGStreamer)
	s.mux.HandleFunc("POST /api/gstreamer/install", s.handleGStreamerInstall)
	s.mux.HandleFunc("POST /api/gstreamer/dismiss", s.handleGStreamerDismiss)
	s.mux.HandleFunc("GET /api/gstreamer", s.handleGStreamerStream)
	s.mux.HandleFunc("POST /api/settings/security/cancel-links", s.handleCancelLinks)
	s.mux.HandleFunc("POST /api/settings/apps", s.handleSwitchApps)
	s.mux.HandleFunc("POST /api/settings/apps/logins", s.handleAddAppLogin)
	s.mux.HandleFunc("POST /api/settings/apps/logins/{user}/revoke", s.handleRevokeAppLogin)
	s.mux.HandleFunc("POST /api/settings/users", s.handleCreateUser)
	s.mux.HandleFunc("POST /api/settings/users/{username}/role/{role}", s.handleUserAction)
	s.mux.HandleFunc("POST /api/settings/users/{username}/{action}", s.handleUserAction)
	s.mux.HandleFunc("GET /api/dashboard", s.handleDashboardStream)
	s.mux.HandleFunc("GET /api/movies/imdb-rating", s.handleMovieIMDbRating)
	s.mux.HandleFunc("GET /api/tv/{id}/season/{season}", s.handleSeason)
	s.mux.HandleFunc("GET /api/search", s.handleSearchAPI)
	s.mux.HandleFunc("GET /api/discover", s.handleDiscover)
	s.mux.HandleFunc("GET /api/torrents", s.handleTorrentSearch)
	s.mux.HandleFunc("POST /api/torrents/add", s.handleTorrentAdd)

	// TorrServer API Endpoints
	s.mux.HandleFunc("GET /api/torrserver/torrents", s.handleTorrServerTorrents)
	s.mux.HandleFunc("POST /api/torrserver/add", s.handleTorrServerAdd)
	s.mux.HandleFunc("POST /api/torrserver/action", s.handleTorrServerAction)
	s.mux.HandleFunc("GET /api/torrserver/files", s.handleTorrServerFiles)
	s.mux.HandleFunc("POST /api/torrserver/files", s.handleTorrServerFiles)
	s.mux.HandleFunc("GET /api/torrserver/probe", s.handleTorrServerProbe)
	s.mux.HandleFunc("GET /api/torrserver/tracks", s.handleTorrServerTracks)
	s.mux.HandleFunc("GET /api/torrserver/playlist", s.handleTorrServerPlaylist)
	s.mux.HandleFunc("GET /api/torrserver/queue", s.handleTorrServerQueue)
	s.mux.HandleFunc("GET /api/torrserver/player-stats", s.handleTorrServerPlayerStats)
	s.mux.HandleFunc("GET /api/torrserver/status", s.handleTorrServerStatus)
	s.mux.HandleFunc("GET /api/torrserver/torrent-stats", s.handleTorrentStats)
	s.mux.HandleFunc("GET /api/torrserver/stream/", s.handleTorrServerStreamProxy)

	// Signed links for external players (VLC, TVs, playlists): no session.
	s.mux.HandleFunc("GET /s/{rest...}", s.handleShareLink)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "ok\n")
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "ready\n")
}

// responseWriterRecorder wraps http.ResponseWriter to capture the status code
// while fully supporting http.Flusher (SSE streaming) and Go 1.20+ ResponseController unwrap.
type responseWriterRecorder struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (rw *responseWriterRecorder) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.wroteHeader = true
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriterRecorder) Write(p []byte) (int, error) {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(p)
}

// Unwrap provides access to the underlying ResponseWriter for http.ResponseController.
func (rw *responseWriterRecorder) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// Flush forwards flush events to the underlying writer, preserving SSE streaming behavior.
func (rw *responseWriterRecorder) Flush() {
	if !rw.wroteHeader {
		rw.WriteHeader(http.StatusOK)
	}
	if flusher, ok := rw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// SecurityHeadersMiddleware adds defensive HTTP security headers according to OWASP guidelines.
func SecurityHeadersMiddleware(secure bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-eval'; style-src 'self'; img-src 'self' data: https: http:; media-src 'self' blob: data: http: https:; frame-src 'self' https://www.youtube-nocookie.com https://www.youtube.com; connect-src 'self' http: https:; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "accelerometer=(), camera=(), geolocation=(), gyroscope=(), magnetometer=(), microphone=(), payment=(), usb=()")
		if secure {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// LoggingMiddleware logs incoming HTTP requests with timing and status code using structured slog.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriterRecorder{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}
		next.ServeHTTP(rw, r)
		// Health checks (Docker's every 30 seconds) are logged only when failing.
		if (r.URL.Path == "/healthz" || r.URL.Path == "/readyz") && rw.statusCode == http.StatusOK {
			return
		}

		slog.Info("http request",
			"method", r.Method,
			"path", logPath(r.URL.Path),
			"status", rw.statusCode,
			"duration", time.Since(start),
		)
	})
}

// logValue is a value from a request as logs show it: line breaks encoded,
// so it cannot start a log line of its own.
func logValue(v string) string {
	return strings.ReplaceAll(strings.ReplaceAll(v, "\r", "%0D"), "\n", "%0A")
}

// logError is an error as logs show it: it can quote request values (a
// hash, an address), so its line breaks are encoded like logValue's.
func logError(err error) string {
	if err == nil {
		return ""
	}
	return logValue(err.Error())
}

// logPath is a request path as logs show it: a share link's token, which
// works for anyone holding it, is left out, and line breaks are shown
// encoded, as they were sent.
func logPath(path string) string {
	path = logValue(path)
	if rest, ok := strings.CutPrefix(path, "/s/"); ok {
		if _, file, found := strings.Cut(rest, "/"); found {
			return "/s/…/" + file
		}
		return "/s/…"
	}
	return path
}

// RecoveryMiddleware gracefully catches panics in any downstream handler,
// logs a structured stack trace via slog, and returns HTTP 500 without crashing the server.
func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &responseWriterRecorder{ResponseWriter: w}
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered in HTTP handler",
					"method", r.Method,
					"path", logPath(r.URL.Path),
					"panic", fmt.Sprintf("%v", rec),
				)
				if !rw.wroteHeader {
					http.Error(rw, "Internal Server Error", http.StatusInternalServerError)
				}
			}
		}()
		next.ServeHTTP(rw, r)
	})
}

func logSSEError(r *http.Request, operation string, err error) {
	if err == nil || errors.Is(err, context.Canceled) || r.Context().Err() != nil {
		return
	}
	slog.Warn("SSE write failed", "operation", operation, "path", logPath(r.URL.Path), "error", err)
}
