package handlers

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/a-h/templ"
	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// followLookups is how many followed series' details are looked up at once
// for the upcoming page.
const followLookups = 6

// Follows lets users follow series and see their upcoming episodes, for
// both apps.
type Follows struct {
	store   store.FollowStore
	user    func(*http.Request) (uid string, ok bool)
	catalog *Catalog
}

// NewFollows keeps followed series in s.
func NewFollows(s store.FollowStore, user func(*http.Request) (string, bool), catalog *Catalog) *Follows {
	return &Follows{store: s, user: user, catalog: catalog}
}

// Register adds the follow routes and the upcoming page to mux.
func (f *Follows) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/follow/state", f.handleState)
	mux.HandleFunc("POST /api/follow", f.handleFollow)
	mux.HandleFunc("DELETE /api/follow", f.handleFollow)
	mux.HandleFunc("GET "+views.CalendarPath, f.handleCalendar)
	mux.HandleFunc("GET /api/home/shows", f.handleHomeShows)
}

// target is the user and the series (?id=) of a request; it answers the
// request itself when there is none.
func (f *Follows) target(w http.ResponseWriter, r *http.Request) (string, int, bool) {
	uid, ok := f.user(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return "", 0, false
	}
	id, ok := positiveInt(r.URL.Query().Get("id"))
	if !ok {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return "", 0, false
	}
	return uid, id, true
}

// handleState patches a series page's follow button.
func (f *Follows) handleState(w http.ResponseWriter, r *http.Request) {
	uid, id, ok := f.target(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	on, err := f.store.Following(ctx, uid, id)
	if err != nil {
		slog.Warn("reading a followed series failed", "error", logError(err))
	}
	f.patchButton(w, r, id, on)
}

// handleFollow follows the series (POST) or stops following it (DELETE).
func (f *Follows) handleFollow(w http.ResponseWriter, r *http.Request) {
	uid, id, ok := f.target(w, r)
	if !ok {
		return
	}
	series := store.FollowedSeries{TMDBID: id}
	on := r.Method == http.MethodPost
	if on { // a series is followed only as TMDB names it
		item, err := f.catalog.Title(r.Context(), "tv", id)
		if errors.Is(err, tmdb.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			slog.Warn("looking up a series on TMDB failed", "error", logError(err))
			http.Error(w, i18n.T(r.Context(), "TMDB is not responding right now. Please try again."), http.StatusBadGateway)
			return
		}
		series.Title, series.Poster = item.Title, item.PosterPath
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	if err := f.store.Follow(ctx, uid, series, on); err != nil {
		slog.Warn("following a series failed", "error", logError(err))
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}
	f.patchButton(w, r, id, on)
}

func (f *Follows) patchButton(w http.ResponseWriter, r *http.Request, id int, on bool) {
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.FollowButton(id, on)); err != nil {
		logSSEError(r, "patch follow button", err)
	}
}

// handleCalendar lists the user's followed series by their next episode:
// dated ones soonest first, then those with no date, then ended ones.
func (f *Follows) handleCalendar(w http.ResponseWriter, r *http.Request) {
	uid, ok := f.user(r)
	if !ok {
		http.Redirect(w, r, f.catalog.signIn, http.StatusSeeOther)
		return
	}
	ctx, cancel := f.catalog.detailsContext(r)
	defer cancel()
	followed, err := f.store.FollowedSeries(ctx, uid)
	if err != nil {
		slog.Warn("reading followed series failed", "error", logError(err))
	}
	upcoming := f.upcoming(ctx, followed)
	w.Header().Set("Cache-Control", "no-store")
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	templ.Handler(views.CalendarPage(f.catalog.userFromRequest(r), upcoming, err != nil)).ServeHTTP(w, r)
}

// lookups are the followed series as TMDB has them, looked up a few at a
// time, in the order followed; nil for one TMDB does not answer for.
func (f *Follows) lookups(ctx context.Context, followed []store.FollowedSeries) []*tmdb.TVDetails {
	out := make([]*tmdb.TVDetails, len(followed))
	details := f.catalog.clients().Details
	if details == nil {
		return out
	}
	var wg sync.WaitGroup
	slots := make(chan struct{}, followLookups)
	for i, s := range followed {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select { // queued lookups stop once the page is given up
			case slots <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-slots }()
			if ctx.Err() != nil {
				return
			}
			if tv, err := details.TV(ctx, s.TMDBID); err == nil {
				out[i] = tv
			}
		}()
	}
	wg.Wait()
	return out
}

// followedItem is the series as a card shows it: named and pictured as TMDB
// has it now, else as it was when followed.
func followedItem(s store.FollowedSeries, tv *tmdb.TVDetails) tmdb.MediaItem {
	item := tmdb.MediaItem{ID: s.TMDBID, Title: s.Title, PosterPath: s.Poster, MediaType: "tv"}
	if tv != nil {
		item.Title, item.PosterPath = cmp.Or(tv.Title, s.Title), cmp.Or(tv.PosterPath, s.Poster)
	}
	return item
}

// upcoming is each followed series' next episode; a series TMDB does not
// answer for stays, with no date.
func (f *Follows) upcoming(ctx context.Context, followed []store.FollowedSeries) []views.UpcomingSeries {
	out := make([]views.UpcomingSeries, len(followed))
	for i, tv := range f.lookups(ctx, followed) {
		out[i] = views.UpcomingSeries{Item: followedItem(followed[i], tv)}
		if tv != nil {
			out[i].Next = tv.NextEpisode
			out[i].Ended = tv.Status == "Ended" || tv.Status == "Canceled"
		}
	}
	rank := func(u views.UpcomingSeries) int {
		switch {
		case u.Ended:
			return 2
		case u.Next.AirDate.IsZero():
			return 1
		}
		return 0
	}
	slices.SortStableFunc(out, func(a, b views.UpcomingSeries) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)), a.Next.AirDate.Compare(b.Next.AirDate))
	})
	return out
}

// handleHomeShows patches the home row of the followed series with an
// episode this week.
func (f *Follows) handleHomeShows(w http.ResponseWriter, r *http.Request) {
	uid, ok := f.user(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	ctx, cancel := f.catalog.detailsContext(r)
	defer cancel()
	followed, err := f.store.FollowedSeries(ctx, uid)
	if err != nil {
		slog.Warn("reading followed series failed", "error", logError(err))
	}
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.ShowsThisWeek(f.thisWeek(ctx, r, followed))); err != nil {
		logSSEError(r, "patch shows this week", err)
	}
}

// thisWeek are the followed series with an episode this week: those whose
// latest episode aired in the last seven days and is not marked watched,
// then those whose next one airs within seven days, soonest first.
func (f *Follows) thisWeek(ctx context.Context, r *http.Request, followed []store.FollowedSeries) []views.WeekShow {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	within := func(day, from, to time.Time) bool {
		return !day.IsZero() && !day.Before(from) && !day.After(to)
	}
	var out []views.WeekShow
	for i, tv := range f.lookups(ctx, followed) {
		if tv == nil {
			continue
		}
		show := views.WeekShow{Item: followedItem(followed[i], tv), TV: tv}
		last, next := tv.LastEpisode, tv.NextEpisode
		aired := last.Number > 0 && within(last.AirDate, today.AddDate(0, 0, -7), today)
		if next.Number > 0 && within(next.AirDate, today, today.AddDate(0, 0, 7)) {
			show.Next = next
		}
		if !aired && show.Next.Number == 0 {
			continue
		}
		show.Marks = f.catalog.episodes.Marks(r, tv.ID)
		if aired && !slices.Contains(show.Marks.Watched[last.Season], last.Number) {
			show.New = last
		}
		if show.New.Number > 0 || show.Next.Number > 0 {
			out = append(out, show)
		}
	}
	slices.SortStableFunc(out, func(a, b views.WeekShow) int {
		if isNew := a.New.Number > 0; isNew != (b.New.Number > 0) {
			if isNew {
				return -1
			}
			return 1
		}
		return a.Next.AirDate.Compare(b.Next.AirDate)
	})
	return out
}
