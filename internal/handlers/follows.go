package handlers

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"sync"

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

// upcoming looks up each followed series' next episode, a few at a time;
// a series TMDB does not answer for stays, with no date.
func (f *Follows) upcoming(ctx context.Context, followed []store.FollowedSeries) []views.UpcomingSeries {
	out := make([]views.UpcomingSeries, len(followed))
	details := f.catalog.clients().Details
	var wg sync.WaitGroup
	slots := make(chan struct{}, followLookups)
	for i, s := range followed {
		out[i] = views.UpcomingSeries{Item: tmdb.MediaItem{ID: s.TMDBID, Title: s.Title, PosterPath: s.Poster, MediaType: "tv"}}
		if details == nil {
			continue
		}
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
			tv, err := details.TV(ctx, s.TMDBID)
			if err != nil {
				return
			}
			out[i].Item.Title = cmp.Or(tv.Title, s.Title)
			out[i].Item.PosterPath = cmp.Or(tv.PosterPath, s.Poster)
			out[i].Next = tv.NextEpisode
			out[i].Ended = tv.Status == "Ended" || tv.Status == "Canceled"
		}()
	}
	wg.Wait()
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
