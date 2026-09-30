// Package web is Moviestracker's cloud web app (cmd/web): the catalog and
// accounts are served from Cloud Run, while each visitor's browser talks to
// their own TorrServer. The server never reaches a visitor's TorrServer.
package web

import (
	"cmp"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/handlers"
	"github.com/lieranderl/moviestracker-app/internal/views"
	webstatic "github.com/lieranderl/moviestracker-app/static"
)

// Config holds what the web app needs to serve requests.
type Config struct {
	// BaseURL is where visitors reach the app (https://… on Cloud Run,
	// http://localhost:8080 locally); Google sends them back under it.
	BaseURL string
	// SessionKey signs the cookies of signed-in visitors.
	SessionKey []byte
	// Google is the OAuth client visitors sign in with.
	Google Google
	// Now is the clock; nil is the wall clock.
	Now func() time.Time
}

// app is the web app's state behind its routes.
type app struct {
	cfg    Config
	signer signer
}

func (a *app) now() time.Time {
	if a.cfg.Now != nil {
		return a.cfg.Now()
	}
	return time.Now()
}

// signInReady says whether visitors can sign in: the Google client and a
// session key long enough to sign with are set.
func (a *app) signInReady() bool {
	return a.cfg.Google.ClientID != "" && a.cfg.Google.ClientSecret != "" && a.cfg.BaseURL != "" && len(a.cfg.SessionKey) >= 32
}

// New returns the web app's HTTP handler: panic recovery, the security
// headers (HSTS too, as Cloud Run serves HTTPS only), request logs,
// cross-origin (CSRF) protection and the visitor's language around the
// routes.
func New(cfg Config) http.Handler {
	cfg.Google.AuthURL = cmp.Or(cfg.Google.AuthURL, "https://accounts.google.com/o/oauth2/v2/auth")
	cfg.Google.TokenURL = cmp.Or(cfg.Google.TokenURL, "https://oauth2.googleapis.com/token")
	a := &app{cfg: cfg, signer: signer{key: cfg.SessionKey}}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "ready\n")
	})
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(webstatic.Files))))
	mux.HandleFunc("GET /{$}", a.handleHome)
	mux.HandleFunc("GET "+signInPath, a.handleSignIn)
	mux.HandleFunc("GET "+signInPath+"/callback", a.handleSignInCallback)
	mux.HandleFunc("POST /api/logout", a.handleSignOut)
	mux.HandleFunc("POST /api/language", handlers.SetLanguage(true))
	app := http.NewCrossOriginProtection().Handler(handlers.Language(mux))
	return handlers.RecoveryMiddleware(handlers.SecurityHeadersMiddleware(true, handlers.LoggingMiddleware(app)))
}

// handleHome is the home page of a signed-in visitor, and the sign-in page
// for the others.
func (a *app) handleHome(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	page := views.WebSignIn()
	if user, ok := a.currentUser(r); ok {
		page = views.WebHome(user.Name, user.Email)
	}
	if err := page.Render(r.Context(), w); err != nil {
		slog.Warn("render failed", "path", r.URL.Path, "error", err)
	}
}

// handleSignOut forgets the visitor's session.
func (a *app) handleSignOut(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, a.cookie(sessionCookie, "", "/", -1))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
