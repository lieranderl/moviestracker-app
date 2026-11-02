package web

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/moviestracker-app/internal/handlers"
	"github.com/lieranderl/moviestracker-app/internal/releases"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// alertPages is how deep into each release feed updates look for releases
// the user has not been through: deep enough to catch up after an absence.
// Releases deeper than that, or gone from the feeds, bring no alert.
const alertPages = 20

// alertFeeds are the feeds a followed movie brings an update from.
var alertFeeds = []releases.Feed{releases.Latest, releases.HDR10, releases.DolbyVision}

// foundAlerts are the followed movies among the feeds' releases found
// since the user's checkpoints, and the checkpoints to keep once those are
// kept: each feed's newest release read. A changed library looks through
// the feeds again, for releases of movies it newly follows.
func (a *app) foundAlerts(ctx context.Context, uid string) ([]store.Update, store.AlertCheckpoints, error) {
	stamp, err := a.cfg.Store.LibraryStamp(ctx, uid)
	if err != nil {
		return nil, store.AlertCheckpoints{}, err
	}
	library, err := a.library.Summary(ctx, uid)
	if err != nil || len(library.Followed) == 0 {
		return nil, store.AlertCheckpoints{}, err
	}
	since, err := a.cfg.Store.AlertCheckpoints(ctx, uid)
	if err != nil {
		return nil, store.AlertCheckpoints{}, err
	}
	if since.Library != stamp {
		since.Feeds = nil
	}
	followed := library.Followed
	var found []store.Update
	checkpoints := store.AlertCheckpoints{Library: stamp, Feeds: map[string]time.Time{}}
	for _, feed := range alertFeeds {
		read := since.Feeds[string(feed)]
	pages:
		for page := 1; page <= alertPages; page++ {
			p, err := a.feeds.Page(ctx, feed, page)
			if err != nil {
				return found, store.AlertCheckpoints{}, err
			}
			for _, it := range p.Items {
				if !it.FoundAt.After(read) && !read.IsZero() {
					break pages // looked through already
				}
				if it.FoundAt.After(checkpoints.Feeds[string(feed)]) {
					checkpoints.Feeds[string(feed)] = it.FoundAt
				}
				if followed[it.ID] {
					found = append(found, store.Update{Feed: string(feed), TMDBID: it.ID, Title: it.Title, Poster: it.PosterPath, FoundAt: a.now()})
				}
			}
			if page >= p.TotalPages {
				break
			}
		}
		if _, ok := checkpoints.Feeds[string(feed)]; !ok && !read.IsZero() {
			checkpoints.Feeds[string(feed)] = read // nothing new
		}
	}
	return found, checkpoints, nil
}

// keepFoundAlerts keeps the alerts the feeds newly hold for the user. The
// navbar asks on every page, so alerts already among the newest are not
// written again. It says whether it kept any.
func (a *app) keepFoundAlerts(ctx context.Context, uid string, newest []store.Update) bool {
	found, checkpoints, err := a.foundAlerts(ctx, uid)
	if err != nil {
		// The checkpoints stay, so what was not read is read next time.
		slog.Warn("finding release alerts failed", "error", handlers.LogError(err))
	}
	known := map[string]bool{}
	for _, u := range newest {
		known[u.ID()] = true
	}
	found = slices.DeleteFunc(found, func(u store.Update) bool { return known[u.ID()] })
	if len(found) > 0 {
		if err := a.cfg.Store.AddUpdates(ctx, uid, found); err != nil {
			slog.Warn("keeping release alerts failed", "error", handlers.LogError(err))
			return false
		}
	}
	// The checkpoints move only once the alerts before them are kept.
	if err == nil && checkpoints.Library != "" {
		if err := a.cfg.Store.SaveAlertCheckpoints(ctx, uid, checkpoints); err != nil {
			slog.Warn("keeping alert checkpoints failed", "error", handlers.LogError(err))
		}
	}
	return len(found) > 0
}

// newestAlerts are the newest page of the user's release alerts, with any
// the feeds newly hold for them kept first, and the older page's cursor.
// An error says the kept alerts could not be read.
func (a *app) newestAlerts(ctx context.Context, uid string) ([]store.Update, string, error) {
	page, next, err := a.cfg.Store.Updates(ctx, uid, "")
	if err != nil || !a.keepFoundAlerts(ctx, uid, page) {
		return page, next, err
	}
	return a.cfg.Store.Updates(ctx, uid, "")
}

// handleAlertsCount patches the navbar's bell with the unseen updates.
func (a *app) handleAlertsCount(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	if _, _, err := a.newestAlerts(ctx, user.ID); err != nil {
		slog.Warn("reading release alerts failed", "error", handlers.LogError(err))
	}
	unseen, err := a.cfg.Store.UnseenUpdates(ctx, user.ID)
	if err != nil {
		slog.Warn("counting release alerts failed", "error", handlers.LogError(err))
	}
	w.Header().Set("Cache-Control", "no-store")
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.AlertsBell(unseen)); err != nil {
		slog.Warn("patching the updates bell failed", "error", err)
	}
}

// handleAlertsPage lists a page of the user's updates (?after= names the
// older pages) and marks those shown seen; the others stay unseen.
func (a *app) handleAlertsPage(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	v := views.AlertsView{Through: a.now()}
	var err error
	if after := r.URL.Query().Get("after"); after == "" {
		v.Alerts, v.Next, err = a.newestAlerts(ctx, user.ID)
	} else {
		v.Alerts, v.Next, err = a.cfg.Store.Updates(ctx, user.ID, after)
	}
	if errors.Is(err, store.ErrCursor) {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err != nil {
		// Alerts the user could not see stay unseen.
		slog.Warn("reading release alerts failed", "error", handlers.LogError(err))
		v.Failed = true
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		var shown []string
		for _, u := range v.Alerts {
			if !u.Seen {
				shown = append(shown, u.ID())
			}
		}
		if err := a.cfg.Store.MarkUpdatesSeen(ctx, user.ID, shown); err != nil {
			slog.Warn("marking release alerts seen failed", "error", handlers.LogError(err))
		}
		if v.Unread, err = a.cfg.Store.UnseenUpdates(ctx, user.ID); err != nil {
			slog.Warn("counting release alerts failed", "error", handlers.LogError(err))
		}
	}
	if err := views.AlertsPage(a.catalogUser(r), v).Render(r.Context(), w); err != nil {
		slog.Warn("render failed", "page", "alerts", "error", err)
	}
}

// handleMarkAllAlertsSeen marks seen the user's updates found up to
// ?through= (Unix nanoseconds: when their alerts page was read), so ones
// arriving later stay unseen, and patches the bell and the offer.
func (a *app) handleMarkAllAlertsSeen(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	nanos, err := strconv.ParseInt(r.URL.Query().Get("through"), 10, 64)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	through := time.Unix(0, nanos)
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	failed := false
	if err := a.cfg.Store.MarkAllUpdatesSeen(ctx, user.ID, through); err != nil {
		slog.Warn("marking all release alerts seen failed", "error", handlers.LogError(err))
		failed = true
	}
	unseen, err := a.cfg.Store.UnseenUpdates(ctx, user.ID)
	if err != nil {
		slog.Warn("counting release alerts failed", "error", handlers.LogError(err))
	}
	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(views.AlertsBell(unseen)); err != nil {
		slog.Warn("patching the updates bell failed", "error", err)
	}
	// Unread alerts found later are not this offer's: it goes once the
	// ones it covered are read.
	left := 0
	if failed {
		left = max(unseen, 1)
	}
	if err := sse.PatchElementTempl(views.AlertsMarkAll(left, through, failed)); err != nil {
		slog.Warn("patching the mark-all offer failed", "error", err)
	}
}
