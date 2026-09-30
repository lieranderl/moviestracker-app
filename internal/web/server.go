// Package web is Moviestracker's cloud web app (cmd/web): the catalog and
// accounts are served from Cloud Run, while each visitor's browser talks to
// their own TorrServer. The server never reaches a visitor's TorrServer.
package web

import (
	"io"
	"log/slog"
	"net/http"

	"github.com/lieranderl/moviestracker-app/internal/handlers"
	"github.com/lieranderl/moviestracker-app/internal/views"
	webstatic "github.com/lieranderl/moviestracker-app/static"
)

// New returns the web app's HTTP handler: panic recovery, the security
// headers (HSTS too, as Cloud Run serves HTTPS only), request logs,
// cross-origin (CSRF) protection and the visitor's language around the
// routes.
func New() http.Handler {
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
	mux.HandleFunc("GET /{$}", handleHome)
	mux.HandleFunc("POST /api/language", handlers.SetLanguage(true))
	app := http.NewCrossOriginProtection().Handler(handlers.Language(mux))
	return handlers.RecoveryMiddleware(handlers.SecurityHeadersMiddleware(true, handlers.LoggingMiddleware(app)))
}

// handleHome is the sign-in page for visitors who are signed out.
func handleHome(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.WebSignIn().Render(r.Context(), w); err != nil {
		slog.Warn("render failed", "page", "sign-in", "error", err)
	}
}
