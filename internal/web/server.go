// Package web is Moviestracker's cloud web app (cmd/web): the catalog and
// accounts are served from Cloud Run, while each visitor's browser talks to
// their own TorrServer. The server never reaches a visitor's TorrServer.
package web

import (
	"io"
	"net/http"

	"github.com/lieranderl/moviestracker-app/internal/handlers"
	webstatic "github.com/lieranderl/moviestracker-app/static"
)

// New returns the web app's HTTP handler: panic recovery, the security
// headers (HSTS too, as Cloud Run serves HTTPS only) and request logs around
// the routes.
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
	return handlers.RecoveryMiddleware(handlers.SecurityHeadersMiddleware(true, handlers.LoggingMiddleware(mux)))
}
