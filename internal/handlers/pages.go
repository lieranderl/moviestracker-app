package handlers

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"

	"github.com/a-h/templ"
)

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	// If already logged in, go straight to the authenticated home
	if user := s.userFromRequest(r); user != nil {
		http.Redirect(w, r, homePath, http.StatusSeeOther)
		return
	}

	// ?review=1 lets a visitor re-read the disclaimer after accepting it.
	consented := hasConsent(r) && r.URL.Query().Get("review") == ""
	templ.Handler(views.Login(consented, "")).ServeHTTP(w, r)
}

func (s *Server) handleMoviesPage(w http.ResponseWriter, r *http.Request) {
	user := s.userFromRequest(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	var catalog tmdb.Catalog
	if s.clients().Catalog != nil {
		ctx, cancel := context.WithTimeout(r.Context(), s.catalogTimeout)
		defer cancel()

		var err error
		catalog, err = s.clients().Catalog.GetCatalog(ctx)
		if err != nil {
			slog.Warn("failed to fetch media catalog", "error", err)
		}
	}

	component := views.Movies(user, catalog.Hero, catalog.Movies, catalog.Series, s.clients().Details != nil)
	templ.Handler(component).ServeHTTP(w, r)
}
