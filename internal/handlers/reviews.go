package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// Reviews serves each user's rating of a title and note on it, for both
// apps. Rating a title marks it watched.
type Reviews struct {
	store   store.ReviewStore
	titles  store.TitleStateStore
	user    func(*http.Request) (uid string, ok bool)
	catalog *Catalog // names titles as TMDB does
}

// NewReviews keeps ratings and notes in s and watch states in titles, for
// the users user names; catalog names the titles.
func NewReviews(s store.ReviewStore, titles store.TitleStateStore, user func(*http.Request) (string, bool), catalog *Catalog) *Reviews {
	return &Reviews{store: s, titles: titles, user: user, catalog: catalog}
}

// Register adds the review routes to mux.
func (rv *Reviews) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/titles/review", rv.handleReview)
	mux.HandleFunc("POST /api/titles/review", rv.handleSave)
}

// handleReview sends a title's page the user's rating and note.
func (rv *Reviews) handleReview(w http.ResponseWriter, r *http.Request) {
	uid, kind, id, ok := titleTarget(w, r, rv.user)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	kept, err := rv.store.Review(ctx, uid, kind, id)
	if err != nil {
		// Shown as none, a rating or note saved then would replace the
		// one kept: editing stays off until it loads.
		slog.Warn("reading a review failed", "error", logError(err))
		w.Header().Set("Cache-Control", "no-store")
		if err := datastar.NewSSE(w, r).PatchElementTempl(views.TitleReviewFailed(kind, id)); err != nil {
			slog.Warn("patching a review error failed", "error", err)
		}
		return
	}
	sse := patchReview(w, r, kind, id, kept, false)
	if err := sse.MarshalAndPatchSignals(map[string]any{"reviewRating": kept.Rating, "reviewNote": kept.Note, "_reviewReady": true}); err != nil {
		slog.Warn("patching review signals failed", "error", err)
	}
}

func patchReview(w http.ResponseWriter, r *http.Request, kind string, id int, rv store.Review, saved bool) *datastar.ServerSentEventGenerator {
	w.Header().Set("Cache-Control", "no-store")
	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(views.TitleReview(kind, id, rv.Rating, saved)); err != nil {
		slog.Warn("patching a review failed", "error", err)
	}
	return sse
}

// handleSave keeps $reviewRating and $reviewNote for a title, named and
// postered as TMDB gives it in the user's language. A rating marks the
// title watched, and its watch-state buttons are patched to say so.
func (rv *Reviews) handleSave(w http.ResponseWriter, r *http.Request) {
	uid, kind, id, ok := titleTarget(w, r, rv.user)
	if !ok {
		return
	}
	var sig struct {
		Rating int    `json:"reviewRating"`
		Note   string `json:"reviewNote"`
	}
	if err := datastar.ReadSignals(r, &sig); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	rec := store.Review{Kind: kind, TMDBID: id, Rating: sig.Rating, Note: sig.Note}
	if rec.Rating < 0 || rec.Rating > 10 {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	item, err := rv.catalog.Title(r.Context(), kind, id)
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
	save, cancel := context.WithTimeout(r.Context(), progressTimeout)
	err = rv.store.SaveReview(save, uid, rec)
	cancel()
	if errors.Is(err, store.ErrReview) {
		http.Error(w, i18n.T(r.Context(), "A note has at most 2000 characters."), http.StatusBadRequest)
		return
	}
	if err != nil {
		slog.Warn("saving a review failed", "error", logError(err))
		http.Error(w, i18n.T(r.Context(), "The change could not be saved. Please try again."), http.StatusServiceUnavailable)
		return
	}
	watched := rec.Rating > 0 && rv.markWatched(r, uid, rec)
	sse := patchReview(w, r, kind, id, rec, true)
	if watched {
		if err := sse.PatchElementTempl(views.WatchStateButtons(kind, id, store.Watched)); err != nil {
			slog.Warn("patching the watch state failed", "error", err)
		}
	}
}

// markWatched marks a rated title watched, and says whether it is now. Its
// own deadline keeps a slow review save from leaving it none.
func (rv *Reviews) markWatched(r *http.Request, uid string, rec store.Review) bool {
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	ts := store.TitleState{Kind: rec.Kind, TMDBID: rec.TMDBID, Title: rec.Title, Poster: rec.Poster, State: store.Watched}
	if kept, err := rv.titles.TitleState(ctx, uid, rec.Kind, rec.TMDBID); err == nil && kept.State == store.Watched {
		return true
	}
	if err := rv.titles.SetTitleState(ctx, uid, ts); err != nil {
		slog.Warn("marking a rated title watched failed", "error", logError(err))
		return false
	}
	return true
}
