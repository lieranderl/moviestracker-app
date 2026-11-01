package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/moviestracker-app/internal/handlers"
	"github.com/lieranderl/moviestracker-app/internal/releases"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// alertPages is how deep into each release feed updates look: its newest
// releases, which the home rows' cache usually holds already.
const alertPages = 5

// alertFeeds are the feeds a followed movie brings an update from.
var alertFeeds = []releases.Feed{releases.Latest, releases.HDR10, releases.DolbyVision}

// followed are the movies the user follows, by TMDB ID: their favourites
// and those they want to watch, but none they watched.
func (a *app) followed(ctx context.Context, uid string) (map[int]bool, error) {
	out := map[int]bool{}
	favs, err := a.cfg.Store.Favorites(ctx, uid)
	if err != nil {
		return nil, err
	}
	for _, f := range favs {
		if f.Kind == "movie" {
			out[f.TMDBID] = true
		}
	}
	want, err := a.cfg.Store.TitleStates(ctx, uid, store.WantToWatch)
	if err != nil {
		return nil, err
	}
	for _, t := range want {
		if t.Kind == "movie" {
			out[t.TMDBID] = true
		}
	}
	watched, err := a.cfg.Store.TitleStates(ctx, uid, store.Watched)
	if err != nil {
		return nil, err
	}
	for _, t := range watched {
		delete(out, t.TMDBID)
	}
	return out, nil
}

// foundAlerts are the followed movies among the feeds' newest releases.
func (a *app) foundAlerts(ctx context.Context, uid string) ([]store.Update, error) {
	followed, err := a.followed(ctx, uid)
	if err != nil || len(followed) == 0 {
		return nil, err
	}
	var found []store.Update
	for _, feed := range alertFeeds {
		for page := 1; page <= alertPages; page++ {
			p, err := a.feeds.Page(ctx, feed, page)
			if err != nil {
				return found, err
			}
			for _, it := range p.Items {
				if followed[it.ID] {
					found = append(found, store.Update{Feed: string(feed), TMDBID: it.ID, Title: it.Title, Poster: it.PosterPath, FoundAt: a.now()})
				}
			}
			if page >= p.TotalPages {
				break
			}
		}
	}
	return found, nil
}

// alerts are the user's release alerts, with any the feeds newly hold for
// them kept first. Only alerts not kept yet are written, as the navbar
// asks on every page.
func (a *app) alerts(r *http.Request, uid string) []store.Update {
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	kept, err := a.cfg.Store.Updates(ctx, uid)
	if err != nil {
		slog.Warn("reading release alerts failed", "error", handlers.LogError(err))
		return nil
	}
	found, err := a.foundAlerts(ctx, uid)
	if err != nil {
		slog.Warn("finding release alerts failed", "error", handlers.LogError(err))
	}
	known := map[string]bool{}
	for _, u := range kept {
		known[fmt.Sprint(u.Feed, u.TMDBID)] = true
	}
	found = slices.DeleteFunc(found, func(u store.Update) bool { return known[fmt.Sprint(u.Feed, u.TMDBID)] })
	if len(found) == 0 {
		return kept
	}
	if err := a.cfg.Store.AddUpdates(ctx, uid, found); err != nil {
		slog.Warn("keeping release alerts failed", "error", handlers.LogError(err))
		return kept
	}
	if kept, err = a.cfg.Store.Updates(ctx, uid); err != nil {
		slog.Warn("reading release alerts failed", "error", handlers.LogError(err))
	}
	return kept
}

// handleAlertsCount patches the navbar's bell with the unseen updates.
func (a *app) handleAlertsCount(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	unseen := 0
	for _, u := range a.alerts(r, user.ID) {
		if !u.Seen {
			unseen++
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.AlertsBell(unseen)); err != nil {
		slog.Warn("patching the updates bell failed", "error", err)
	}
}

// handleAlertsPage lists the user's updates and marks them seen.
func (a *app) handleAlertsPage(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	updates := a.alerts(r, user.ID)
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	if err := a.cfg.Store.MarkUpdatesSeen(ctx, user.ID); err != nil {
		slog.Warn("marking updates seen failed", "error", handlers.LogError(err))
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.AlertsPage(a.catalogUser(r), updates).Render(r.Context(), w); err != nil {
		slog.Warn("render failed", "page", "alerts", "error", err)
	}
}
