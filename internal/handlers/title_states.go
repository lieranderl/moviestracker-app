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
	mux.HandleFunc("GET /lists/{list}", ts.handleList)
}

// handleList is My lists' Want to watch or Watched tab.
func (ts *TitleStates) handleList(w http.ResponseWriter, r *http.Request) {
	list := r.PathValue("list")
	if list != views.ListWant && list != views.ListWatched {
		http.NotFound(w, r)
		return
	}
	uid, ok := ts.user(r)
	if !ok {
		http.Redirect(w, r, ts.catalog.signIn, http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	kept, err := ts.store.TitleStates(ctx, uid, list)
	if err != nil {
		slog.Warn("reading watch states failed", "error", logError(err))
	}
	items := make([]tmdb.MediaItem, 0, len(kept))
	for _, t := range kept {
		items = append(items, tmdb.MediaItem{ID: t.TMDBID, Title: t.Title, PosterPath: t.Poster, MediaType: t.Kind})
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	if err := views.ListsPage(ts.catalog.userFromRequest(r), list, items, err != nil).Render(r.Context(), w); err != nil {
		slog.Warn("render failed", "page", "lists", "error", err)
	}
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

// markWatched marks a movie the user finished watched, named as TMDB names
// it (as the torrent was titled when TMDB does not answer). One already
// watched stays as it was. Each store call has its own deadline, so a slow
// TMDB lookup between them does not leave the write none.
func (ts *TitleStates) markWatched(r *http.Request, uid string, id int, title string) {
	read, cancel := context.WithTimeout(r.Context(), progressTimeout)
	kept, err := ts.store.TitleState(read, uid, "movie", id)
	cancel()
	if err == nil && kept.State == store.Watched {
		return
	}
	rec := store.TitleState{Kind: "movie", TMDBID: id, Title: title, State: store.Watched}
	if item, err := ts.catalog.Title(r.Context(), "movie", id); err == nil {
		rec.Title, rec.Poster = item.Title, item.PosterPath
	}
	write, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	if err := ts.store.SetTitleState(write, uid, rec); err != nil {
		slog.Warn("marking a finished movie watched failed", "error", logError(err))
	}
}
