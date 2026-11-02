package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

// quickNotice is what a card's quick action shows in the card toast
// (views.cardToast): a message, whether it is an error, and the address
// that undoes the action ("" for none).
type quickNotice struct {
	Toast string `json:"_quickToast"`
	Error bool   `json:"_quickError"`
	Undo  string `json:"_quickUndo"`
}

func validState(state string) bool {
	return state == "" || state == store.WantToWatch || state == store.Watched
}

// named is rec named and postered as TMDB gives its title in the user's
// language; ok=false when TMDB does not, the request answered.
func (ts *TitleStates) named(w http.ResponseWriter, r *http.Request, rec store.TitleState) (store.TitleState, bool) {
	item, err := ts.catalog.Title(r.Context(), rec.Kind, rec.TMDBID)
	if errors.Is(err, tmdb.ErrNotFound) {
		http.NotFound(w, r)
		return rec, false
	}
	if err != nil {
		slog.Warn("looking up a title on TMDB failed", "error", logError(err))
		patchQuickNotice(w, r, quickNotice{Toast: i18n.T(r.Context(), "TMDB is not responding right now. Please try again."), Error: true})
		return rec, false
	}
	rec.Title, rec.Poster = item.Title, item.PosterPath
	return rec, true
}

// handleQuick gives a title the state a card's quick action posts
// (?state=want|watched) and offers to undo it: to put back the state it
// had, only while the title still has the new one.
func (ts *TitleStates) handleQuick(w http.ResponseWriter, r *http.Request) {
	uid, kind, id, ok := ts.target(w, r)
	if !ok {
		return
	}
	state := r.URL.Query().Get("state")
	if state != store.WantToWatch && state != store.Watched {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	read, cancel := context.WithTimeout(r.Context(), progressTimeout)
	prior, err := ts.store.TitleState(read, uid, kind, id)
	cancel()
	if err != nil {
		slog.Warn("reading a watch state failed", "error", logError(err))
		patchQuickNotice(w, r, quickNotice{Toast: i18n.T(r.Context(), "The change could not be saved. Please try again."), Error: true})
		return
	}
	if prior.State == state {
		patchQuickNotice(w, r, quickNotice{Toast: alreadyIn(r.Context(), state)})
		return
	}
	rec, ok := ts.named(w, r, store.TitleState{Kind: kind, TMDBID: id, State: state})
	if !ok {
		return
	}
	if !ts.change(w, r, uid, rec, prior.State) {
		return
	}
	undo := url.Values{"type": {kind}, "id": {strconv.Itoa(id)}, "from": {state}, "to": {prior.State}}
	if !prior.UpdatedAt.IsZero() {
		undo.Set("at", strconv.FormatInt(prior.UpdatedAt.UnixNano(), 10))
	}
	patchQuickNotice(w, r, quickNotice{Toast: addedTo(r.Context(), state), Undo: "/api/titles/undo?" + undo.Encode()})
}

// handleUndo puts back the state a quick action changed (?to=, marked at
// ?at= in Unix nanoseconds), only while the title still has the one it gave
// (?from=).
func (ts *TitleStates) handleUndo(w http.ResponseWriter, r *http.Request) {
	uid, kind, id, ok := ts.target(w, r)
	if !ok {
		return
	}
	q := r.URL.Query()
	from, to := q.Get("from"), q.Get("to")
	if !validState(from) || !validState(to) {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	rec := store.TitleState{Kind: kind, TMDBID: id, State: to}
	if at, err := strconv.ParseInt(q.Get("at"), 10, 64); err == nil {
		rec.UpdatedAt = time.Unix(0, at)
	}
	if to != "" {
		if rec, ok = ts.named(w, r, rec); !ok {
			return
		}
	}
	if ts.change(w, r, uid, rec, from) {
		patchQuickNotice(w, r, quickNotice{Toast: i18n.T(r.Context(), "Undone")})
	}
}

// change gives the title rec's state while it still has from, and says
// whether it did; otherwise it tells the user why.
func (ts *TitleStates) change(w http.ResponseWriter, r *http.Request, uid string, rec store.TitleState, from string) bool {
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	err := ts.store.ChangeTitleState(ctx, uid, rec, from)
	if errors.Is(err, store.ErrTitleStateChanged) {
		patchQuickNotice(w, r, quickNotice{Toast: i18n.T(r.Context(), "This title changed elsewhere, so it was left as it is."), Error: true})
		return false
	}
	if err != nil {
		slog.Warn("saving a watch state failed", "error", logError(err))
		patchQuickNotice(w, r, quickNotice{Toast: i18n.T(r.Context(), "The change could not be saved. Please try again."), Error: true})
		return false
	}
	return true
}

func addedTo(ctx context.Context, state string) string {
	if state == store.Watched {
		return i18n.T(ctx, "Marked as watched")
	}
	return i18n.T(ctx, "Added to Want to watch")
}

func alreadyIn(ctx context.Context, state string) string {
	if state == store.Watched {
		return i18n.T(ctx, "Already marked as watched")
	}
	return i18n.T(ctx, "Already in Want to watch")
}

func patchQuickNotice(w http.ResponseWriter, r *http.Request, n quickNotice) {
	w.Header().Set("Cache-Control", "no-store")
	if err := datastar.NewSSE(w, r).MarshalAndPatchSignals(n); err != nil {
		slog.Warn(fmt.Sprintf("patching the card toast failed: %v", err))
	}
}
