package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/moviestracker-app/internal/handlers"
	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// favoriteTarget is the signed-in user and the title (?type=movie|tv&id=N)
// of a favourites request. It answers the request itself (401, 404) and
// returns ok=false when there is none.
func (a *app) favoriteTarget(w http.ResponseWriter, r *http.Request) (user User, kind string, id int, ok bool) {
	user, ok = a.currentUser(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return User{}, "", 0, false
	}
	kind = r.URL.Query().Get("type")
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if (kind != "movie" && kind != "tv") || err != nil || id <= 0 {
		http.NotFound(w, r)
		return User{}, "", 0, false
	}
	return user, kind, id, true
}

// patchFavorite sends the title's favourite button in its state.
func patchFavorite(w http.ResponseWriter, r *http.Request, kind string, id int, on bool) {
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.FavoriteButton(kind, id, on)); err != nil {
		slog.Warn("patching the favourite button failed", "error", err)
	}
}

// handleFavoriteState sends a title page its favourite button.
func (a *app) handleFavoriteState(w http.ResponseWriter, r *http.Request) {
	user, kind, id, ok := a.favoriteTarget(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	on, err := a.cfg.Store.IsFavorite(ctx, user.ID, kind, id)
	if err != nil {
		slog.Warn("reading a favourite failed", "error", handlers.LogError(err))
	}
	patchFavorite(w, r, kind, id, on)
}

// handleAddFavorite keeps a title TMDB knows as the user's favourite, with
// its title and poster as TMDB gives them in the user's language.
func (a *app) handleAddFavorite(w http.ResponseWriter, r *http.Request) {
	user, kind, id, ok := a.favoriteTarget(w, r)
	if !ok {
		return
	}
	item, err := a.title(r.Context(), kind, id)
	if errors.Is(err, tmdb.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		slog.Warn("looking up a favourite on TMDB failed", "error", handlers.LogError(err))
		http.Error(w, i18n.T(r.Context(), "TMDB is not responding right now. Please try again."), http.StatusBadGateway)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	if err := a.cfg.Store.AddFavorite(ctx, user.ID, store.Favorite{Kind: kind, TMDBID: id, Title: item.Title, Poster: item.PosterPath}); err != nil {
		slog.Warn("adding a favourite failed", "error", handlers.LogError(err))
		http.Error(w, i18n.T(r.Context(), "The favourite could not be saved. Please try again."), http.StatusServiceUnavailable)
		return
	}
	patchFavorite(w, r, kind, id, true)
}

// handleRemoveFavorite forgets one of the user's favourites.
func (a *app) handleRemoveFavorite(w http.ResponseWriter, r *http.Request) {
	user, kind, id, ok := a.favoriteTarget(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	if err := a.cfg.Store.RemoveFavorite(ctx, user.ID, kind, id); err != nil {
		slog.Warn("removing a favourite failed", "error", handlers.LogError(err))
		http.Error(w, i18n.T(r.Context(), "The favourite could not be removed. Please try again."), http.StatusServiceUnavailable)
		return
	}
	patchFavorite(w, r, kind, id, false)
}

// handleFavoritesPage lists the user's favourites.
func (a *app) handleFavoritesPage(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	favs, err := a.cfg.Store.Favorites(ctx, user.ID)
	if err != nil {
		slog.Warn("reading favourites failed", "error", handlers.LogError(err))
	}
	items := make([]tmdb.MediaItem, 0, len(favs))
	for _, f := range favs {
		items = append(items, tmdb.MediaItem{ID: f.TMDBID, Title: f.Title, PosterPath: f.Poster, MediaType: f.Kind})
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	if err := views.FavoritesPage(a.catalogUser(r), items, err != nil).Render(r.Context(), w); err != nil {
		slog.Warn("render failed", "page", "favourites", "error", err)
	}
}

// title is the movie or series TMDB knows by id.
func (a *app) title(ctx context.Context, kind string, id int) (tmdb.MediaItem, error) {
	details := a.cfg.Sources.Details
	if details == nil {
		return tmdb.MediaItem{}, errors.New("tmdb is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, catalogTimeout)
	defer cancel()
	if kind == "tv" {
		tv, err := details.TV(ctx, id)
		if err != nil {
			return tmdb.MediaItem{}, err
		}
		return tv.MediaItem, nil
	}
	movie, err := details.Movie(ctx, id)
	if err != nil {
		return tmdb.MediaItem{}, err
	}
	return movie.MediaItem, nil
}
