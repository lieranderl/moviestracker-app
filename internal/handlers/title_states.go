package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// TitleStates serves the titles users want to watch or have watched, for
// both apps.
type TitleStates struct {
	store   store.TitleStateStore
	user    func(*http.Request) (uid string, ok bool)
	catalog *Catalog // names titles as TMDB does
}

// NewTitleStates keeps watch states in s for the users user names; catalog
// names the titles.
func NewTitleStates(s store.TitleStateStore, user func(*http.Request) (string, bool), catalog *Catalog) *TitleStates {
	return &TitleStates{store: s, user: user, catalog: catalog}
}

// Register adds the watch-state routes to mux.
func (ts *TitleStates) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/titles/state", ts.handleState)
	mux.HandleFunc("POST /api/titles/state", ts.handleSet)
	mux.HandleFunc("DELETE /api/titles/state", ts.handleSet)
}

// target is the signed-in user and the title (?type=movie|tv&id=N) of a
// request. It answers the request itself (401, 400) and returns ok=false
// when there is none.
func (ts *TitleStates) target(w http.ResponseWriter, r *http.Request) (uid, kind string, id int, ok bool) {
	uid, ok = ts.user(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return "", "", 0, false
	}
	kind = r.URL.Query().Get("type")
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if (kind != "movie" && kind != "tv") || err != nil || id <= 0 {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return "", "", 0, false
	}
	return uid, kind, id, true
}

func patchWatchState(w http.ResponseWriter, r *http.Request, kind string, id int, state string) {
	w.Header().Set("Cache-Control", "no-store")
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.WatchStateButtons(kind, id, state)); err != nil {
		slog.Warn("patching the watch state failed", "error", err)
	}
}

// handleState sends a title page its watch-state buttons.
func (ts *TitleStates) handleState(w http.ResponseWriter, r *http.Request) {
	uid, kind, id, ok := ts.target(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	kept, err := ts.store.TitleState(ctx, uid, kind, id)
	if err != nil {
		slog.Warn("reading a watch state failed", "error", logError(err))
	}
	patchWatchState(w, r, kind, id, kept.State)
}

// handleSet gives a title the posted state (?state=want|watched), named
// and postered as TMDB gives it in the user's language, or clears it
// (DELETE).
func (ts *TitleStates) handleSet(w http.ResponseWriter, r *http.Request) {
	uid, kind, id, ok := ts.target(w, r)
	if !ok {
		return
	}
	rec := store.TitleState{Kind: kind, TMDBID: id}
	if r.Method == http.MethodPost {
		rec.State = r.URL.Query().Get("state")
		if rec.State != store.WantToWatch && rec.State != store.Watched {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		item, err := ts.catalog.Title(r.Context(), kind, id)
		if errors.Is(err, tmdb.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			slog.Warn("looking up a title on TMDB failed", "error", logError(err))
			http.Error(w, i18n.T(r.Context(), "TMDB is not responding right now. Please try again."), http.StatusBadGateway)
			return
		}
		rec.Title, rec.Poster = item.Title, item.PosterPath
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	if err := ts.store.SetTitleState(ctx, uid, rec); err != nil {
		slog.Warn("saving a watch state failed", "error", logError(err))
		http.Error(w, i18n.T(r.Context(), "The change could not be saved. Please try again."), http.StatusServiceUnavailable)
		return
	}
	patchWatchState(w, r, kind, id, rec.State)
}
