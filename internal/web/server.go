// Package web is Moviestracker's cloud web app (cmd/web): the catalog and
// accounts are served from Cloud Run, while each visitor's browser talks to
// their own TorrServer. The server never reaches a visitor's TorrServer.
package web

import (
	"cmp"
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/auth"
	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/handlers"
	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/sources"
	"github.com/lieranderl/moviestracker-app/internal/store"
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
	// Store keeps each user's preferences and favourites; nil keeps them in
	// memory until the app stops.
	Store store.Store
	// Sources are the TMDB, JacRed and IMDb clients of the catalog; a nil
	// one is a service not configured.
	Sources sources.Clients
	// Now is the clock; nil is the wall clock.
	Now func() time.Time
}

// app is the web app's state behind its routes.
type app struct {
	cfg     Config
	signer  signer
	catalog *handlers.Catalog
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
	if cfg.Store == nil {
		cfg.Store = store.NewMemory()
	}
	a := &app{cfg: cfg, signer: signer{key: cfg.SessionKey}}
	a.catalog = handlers.NewCatalog(func() *sources.Clients { return &a.cfg.Sources }, a.catalogUser, "/", catalogTimeout)

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
	a.catalog.Register(mux, "")
	mux.HandleFunc("GET "+signInPath, a.handleSignIn)
	mux.HandleFunc("GET "+signInPath+"/callback", a.handleSignInCallback)
	mux.HandleFunc("POST /api/logout", a.handleSignOut)
	mux.HandleFunc("POST /api/language", handlers.SetLanguage(a.secure(), a.saveLanguage))
	app := http.NewCrossOriginProtection().Handler(handlers.Language(webSite(mux)))
	return handlers.RecoveryMiddleware(handlers.SecurityHeadersMiddleware(true, handlers.LoggingMiddleware(app)))
}

// handleHome is the catalog's home page for a signed-in visitor, and the
// sign-in page for the others.
func (a *app) handleHome(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if _, ok := a.currentUser(r); ok {
		a.catalog.Home(w, r)
		return
	}
	if err := views.WebSignIn().Render(r.Context(), w); err != nil {
		slog.Warn("render failed", "page", "home", "error", err)
	}
}

// saveLanguage keeps the language a signed-in user picked, so their other
// browsers follow it when they sign in there.
func (a *app) saveLanguage(r *http.Request, lang i18n.Lang) {
	user, ok := a.currentUser(r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	if err := a.cfg.Store.SavePreferences(ctx, user.ID, store.Preferences{Language: string(lang)}); err != nil {
		slog.Warn("saving a user's language failed", "error", err)
	}
}

// handleSignOut forgets the visitor's session.
func (a *app) handleSignOut(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, a.cookie(sessionCookie, "", "/", -1))
	handlers.Navigate(w, r, "/")
}

// catalogTimeout bounds the TMDB lookups of one catalog page, as in the
// local app.
const catalogTimeout = 8 * time.Second

// catalogUser is the signed-in visitor as the catalog's pages show them:
// their Google name and email, never an administrator.
func (a *app) catalogUser(r *http.Request) *auth.User {
	user, ok := a.currentUser(r)
	if !ok {
		return nil
	}
	return &auth.User{Username: user.Email, Name: user.Name, Role: config.RoleViewer}
}

// webSite marks every page as the web app's, so shared views show its
// navigation.
func webSite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(views.WithSite(r.Context(), views.Site{Cloud: true})))
	})
}
