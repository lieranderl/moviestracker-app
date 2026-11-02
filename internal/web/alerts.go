package web

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/moviestracker-app/internal/handlers"
	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/releases"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
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
	if err != nil {
		return nil, store.AlertCheckpoints{}, err
	}
	since, err := a.cfg.Store.AlertCheckpoints(ctx, uid)
	if err != nil {
		return nil, store.AlertCheckpoints{}, err
	}
	if since.Library != stamp {
		since.Feeds = nil
	}
	muted, err := a.cfg.Store.MutedAlerts(ctx, uid)
	if err != nil {
		return nil, store.AlertCheckpoints{}, err
	}
	choices, err := a.cfg.Store.AlertFeeds(ctx, uid)
	if err != nil {
		return nil, store.AlertCheckpoints{}, err
	}
	followed := library.Followed
	if len(muted) > 0 { // muted movies bring no alerts; the summary's map stays as kept
		followed = maps.Clone(followed)
		for _, m := range muted {
			delete(followed, m.TMDBID)
		}
	}
	checkpoints := store.AlertCheckpoints{Library: stamp, Feeds: map[string]time.Time{}}
	var found []store.Update
	for _, feed := range alertFeeds {
		read := since.Feeds[string(feed)]
		if len(followed) == 0 { // nothing to look for; the feed is read once there is
			if !read.IsZero() {
				checkpoints.Feeds[string(feed)] = read
			}
			continue
		}
		// The newest page usually holds all that is new, and the home rows
		// keep it. A deeper look reads the backend afresh from the first
		// page: kept pages may be of different ages, and a fresh first page
		// before an older second would skip the releases between them.
		choice := choices[string(feed)]
		scan, err := a.scanFeed(ctx, a.feeds.Page, feed, read, followed, choice.On, 1)
		if err == nil && !scan.done {
			scan, err = a.scanFeed(ctx, a.feeds.src.Page, feed, read, followed, choice.On, alertPages)
		}
		if !choice.Off { // a feed turned off is still read, so it alerts only for later releases once on
			found = append(found, scan.found...)
		}
		if err != nil {
			return found, store.AlertCheckpoints{}, err
		}
		switch {
		case !scan.newest.IsZero():
			checkpoints.Feeds[string(feed)] = scan.newest
		case !read.IsZero():
			checkpoints.Feeds[string(feed)] = read // nothing new
		}
	}
	episodes, checked, err := a.airedEpisodes(ctx, uid, since.Feeds[store.EpisodesFeed], off[store.EpisodesFeed])
	found = append(found, episodes...)
	if err != nil {
		return found, store.AlertCheckpoints{}, err
	}
	if !checked.IsZero() {
		checkpoints.Feeds[store.EpisodesFeed] = checked
	}
	if len(checkpoints.Feeds) == 0 {
		checkpoints.Library = "" // nothing read: nothing to keep
	}
	return found, checkpoints, nil
}

// episodeCheckEvery is how often a user's followed series are looked up on
// TMDB for episodes that aired.
const episodeCheckEvery = time.Hour

// airedEpisodes are the latest episodes of the user's followed series that
// aired since they followed each and since the last check (last), and when
// this check was made. A recent check, or the feed turned off, looks
// nothing up; turned off, the check still moves, so turned on again only
// later episodes alert.
func (a *app) airedEpisodes(ctx context.Context, uid string, last time.Time, off bool) ([]store.Update, time.Time, error) {
	now := a.now()
	if !last.IsZero() && now.Sub(last) < episodeCheckEvery {
		return nil, last, nil
	}
	follows, err := a.cfg.Store.FollowedSeries(ctx, uid)
	if err != nil || len(follows) == 0 || a.cfg.Sources.Details == nil {
		return nil, time.Time{}, err
	}
	if off {
		return nil, now, nil
	}
	day := func(t time.Time) time.Time { return t.UTC().Truncate(24 * time.Hour) }
	var found []store.Update
	for _, s := range follows {
		tv, err := a.cfg.Sources.Details.TV(ctx, s.TMDBID)
		if err != nil {
			slog.Warn("looking up a followed series failed", "tv", s.TMDBID, "error", handlers.LogError(err))
			continue
		}
		ep := tv.LastEpisode
		aired := day(ep.AirDate)
		if ep.AirDate.IsZero() || aired.After(now) || aired.Before(day(s.At)) || (!last.IsZero() && aired.Before(day(last))) {
			continue
		}
		found = append(found, store.Update{
			Feed: store.EpisodesFeed, TMDBID: s.TMDBID, Episode: fmt.Sprintf("S%02dE%02d", ep.Season, ep.Number),
			Title: cmp.Or(tv.Title, s.Title), Poster: cmp.Or(tv.PosterPath, s.Poster), FoundAt: now,
		})
	}
	return found, now, nil
}

// feedScan is what a look through a feed found: the followed movies, the
// newest release read, and whether it reached what was looked through
// before or the feed's end.
type feedScan struct {
	found  []store.Update
	newest time.Time
	done   bool
}

// scanFeed looks through up to pages pages of feed, read by page, for the
// followed movies found after read; none found by quiet (when the feed was
// turned back on) alerts.
func (a *app) scanFeed(ctx context.Context, page func(context.Context, releases.Feed, int) (tmdb.Page, error), feed releases.Feed, read time.Time, followed map[int]bool, quiet time.Time, pages int) (feedScan, error) {
	var scan feedScan
	for n := 1; n <= pages; n++ {
		p, err := page(ctx, feed, n)
		if err != nil {
			return scan, err
		}
		for _, it := range p.Items {
			if !read.IsZero() && !it.FoundAt.After(read) {
				scan.done = true // looked through already
				return scan, nil
			}
			if it.FoundAt.After(scan.newest) {
				scan.newest = it.FoundAt
			}
			if followed[it.ID] && it.FoundAt.After(quiet) {
				scan.found = append(scan.found, store.Update{Feed: string(feed), TMDBID: it.ID, Title: it.Title, Poster: it.PosterPath, FoundAt: a.now()})
			}
		}
		if n >= p.TotalPages {
			scan.done = true
			return scan, nil
		}
	}
	return scan, nil
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
	if err == nil && checkpoints.Feeds != nil {
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
	if v.Muted, err = a.cfg.Store.MutedAlerts(ctx, user.ID); err != nil {
		slog.Warn("reading muted alerts failed", "error", handlers.LogError(err))
	}
	choices, err := a.cfg.Store.AlertFeeds(ctx, user.ID)
	if err != nil {
		slog.Warn("reading alert feeds failed", "error", handlers.LogError(err))
	}
	v.FeedsOff = map[string]bool{}
	for feed, c := range choices {
		v.FeedsOff[feed] = c.Off
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

// handleMuteAlerts stops (?on=1) or resumes (?on=0) release alerts for a
// movie (?id=), and tells the page which way it went. Alerts already found
// for it stay as they are.
func (a *app) handleMuteAlerts(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if err != nil || id <= 0 {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	on := r.URL.Query().Get("on") == "1"
	m := store.MutedTitle{TMDBID: id}
	if on { // named for the list of muted movies; unnamed when TMDB is slow
		if item, err := a.title(r.Context(), "movie", id); err == nil {
			m.Title = item.Title
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	if err := a.cfg.Store.MuteAlerts(ctx, uid, m, on); err != nil {
		slog.Warn("muting release alerts failed", "error", handlers.LogError(err))
		http.Error(w, i18n.T(r.Context(), "The change could not be saved. Please try again."), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if err := datastar.NewSSE(w, r).MarshalAndPatchSignals(map[string]any{"_muted": map[string]bool{strconv.Itoa(id): on}}); err != nil {
		slog.Warn("patching muted alerts failed", "error", err)
	}
}

// handleAlertFeed turns a release feed's alerts on (?on=1) or off (?on=0)
// (?feed=), and tells the page which way it went.
func (a *app) handleAlertFeed(w http.ResponseWriter, r *http.Request) {
	uid, ok := a.userID(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	feed := r.URL.Query().Get("feed")
	if !slices.Contains(alertFeeds, releases.Feed(feed)) && feed != store.EpisodesFeed {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	on := r.URL.Query().Get("on") == "1"
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	if err := a.cfg.Store.SetAlertFeed(ctx, uid, feed, on); err != nil {
		slog.Warn("setting an alert feed failed", "error", handlers.LogError(err))
		http.Error(w, i18n.T(r.Context(), "The change could not be saved. Please try again."), http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if err := datastar.NewSSE(w, r).MarshalAndPatchSignals(map[string]any{"_feedsOn": map[string]bool{feed: on}}); err != nil {
		slog.Warn("patching alert feeds failed", "error", err)
	}
}
