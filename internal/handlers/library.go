package handlers

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"sync"

	"github.com/lieranderl/moviestracker-app/internal/store"
)

// libraryCacheSize is how many users' library summaries a Library keeps.
const libraryCacheSize = 2000

// LibrarySummary is what recommendations and release alerts need of a
// user's library, worked out from all of it.
type LibrarySummary struct {
	// Seeds are the titles recommendations start from, at most
	// recommendationSeeds: watched ones first, then favourites and Want to
	// watch, newest first.
	Seeds []seed
	// Marked is every title the user marked, by titleKey.
	Marked map[string]bool
	// Followed are the movies release alerts follow, by TMDB ID: favourites
	// and Want to watch, but none watched.
	Followed map[int]bool
	// Hidden is how many titles the user is not interested in; Given, how
	// many have feedback of any kind.
	Hidden, Given int
}

// Library works out users' library summaries. With a stamp (the store's
// LibraryStamp), it keeps each one until the user's library changes, so a
// page asking for it reads one stamp instead of the whole library.
type Library struct {
	titles store.TitleStateStore
	// favorites are the user's favourites; nil where none are kept (the
	// local app).
	favorites func(ctx context.Context, uid string) ([]store.Favorite, error)
	stamp     func(ctx context.Context, uid string) (string, error)
	// feedback and reviews shape recommendations when set: titles the user
	// is not interested in or wants more like, and their ratings.
	feedback store.FeedbackStore
	reviews  store.ReviewStore

	mu    sync.Mutex
	cache map[string]keptSummary // by user
}

type keptSummary struct {
	stamp   string
	summary LibrarySummary
}

// NewLibrary summarises the titles users marked in titles and, when
// favorites is set, their favourites; stamp, when set, lets summaries be
// kept until the library changes.
func NewLibrary(titles store.TitleStateStore, favorites func(context.Context, string) ([]store.Favorite, error), stamp func(context.Context, string) (string, error)) *Library {
	return &Library{titles: titles, favorites: favorites, stamp: stamp, cache: map[string]keptSummary{}}
}

// Consider makes summaries follow the users' recommendation feedback and
// ratings.
func (l *Library) Consider(feedback store.FeedbackStore, reviews store.ReviewStore) {
	l.feedback, l.reviews = feedback, reviews
}

// Summary is the user's library summary. Callers must not change its maps.
func (l *Library) Summary(ctx context.Context, uid string) (LibrarySummary, error) {
	stamp, stamped := "", false
	if l.stamp != nil {
		var err error
		stamp, err = l.stamp(ctx, uid)
		stamped = err == nil
	}
	if stamped {
		l.mu.Lock()
		kept, ok := l.cache[uid]
		l.mu.Unlock()
		if ok && kept.stamp == stamp {
			return kept.summary, nil
		}
	}
	summary, err := l.summarise(ctx, uid)
	if err != nil || !stamped {
		return summary, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.cache[uid]; !ok && len(l.cache) >= libraryCacheSize {
		for other := range l.cache { // any one makes room
			delete(l.cache, other)
			break
		}
	}
	l.cache[uid] = keptSummary{stamp, summary}
	return summary, nil
}

// summarise reads the user's whole library.
func (l *Library) summarise(ctx context.Context, uid string) (LibrarySummary, error) {
	s := LibrarySummary{Marked: map[string]bool{}, Followed: map[int]bool{}}
	add := func(sd seed) {
		if !s.Marked[titleKey(sd.kind, sd.id)] && len(s.Seeds) < recommendationSeeds {
			s.Seeds = append(s.Seeds, sd)
		}
		s.Marked[titleKey(sd.kind, sd.id)] = true
	}
	if l.feedback != nil {
		hidden, err := l.feedback.FeedbackOn(ctx, uid, store.NotInterested)
		if err != nil {
			return LibrarySummary{}, err
		}
		for _, t := range hidden {
			s.Marked[titleKey(t.Kind, t.TMDBID)] = true
		}
		s.Hidden = len(hidden)
		more, err := l.feedback.FeedbackOn(ctx, uid, store.MoreLikeThis)
		if err != nil {
			return LibrarySummary{}, err
		}
		for _, t := range more {
			add(seed{kind: t.Kind, id: t.TMDBID, title: t.Title, more: true})
		}
		s.Given = len(hidden) + len(more)
	}
	watched, err := l.titles.TitleStates(ctx, uid, store.Watched)
	if err != nil {
		return LibrarySummary{}, err
	}
	ratings, err := l.ratings(ctx, uid)
	if err != nil { // a summary without them would be kept as if complete
		return LibrarySummary{}, err
	}
	liked := slices.Clone(watched)
	// Rated titles lead, best first; one rated 4 or less starts no row.
	liked = slices.DeleteFunc(liked, func(t store.TitleState) bool {
		r := ratings[titleKey(t.Kind, t.TMDBID)]
		if r > 0 && r <= 4 {
			s.Marked[titleKey(t.Kind, t.TMDBID)] = true
			return true
		}
		return false
	})
	slices.SortStableFunc(liked, func(a, b store.TitleState) int {
		return cmp.Compare(rankOf(ratings, b), rankOf(ratings, a))
	})
	for _, t := range liked {
		add(seed{kind: t.Kind, id: t.TMDBID, title: t.Title, watched: true})
	}
	if l.favorites != nil {
		favs, err := l.favorites(ctx, uid)
		if err != nil {
			return LibrarySummary{}, err
		}
		for _, f := range favs {
			add(seed{kind: f.Kind, id: f.TMDBID, title: f.Title})
			if f.Kind == "movie" {
				s.Followed[f.TMDBID] = true
			}
		}
	}
	want, err := l.titles.TitleStates(ctx, uid, store.WantToWatch)
	if err != nil {
		return LibrarySummary{}, err
	}
	for _, t := range want {
		add(seed{kind: t.Kind, id: t.TMDBID, title: t.Title})
		if t.Kind == "movie" {
			s.Followed[t.TMDBID] = true
		}
	}
	for _, t := range watched {
		if t.Kind == "movie" {
			delete(s.Followed, t.TMDBID)
		}
	}
	return s, nil
}

// MarkedFor is the request's user's marked titles (Summary's Marked),
// for keeping them out of picks; none when signed out or unreadable.
func (l *Library) MarkedFor(user func(*http.Request) (string, bool)) func(*http.Request) map[string]bool {
	return func(r *http.Request) map[string]bool {
		uid, ok := user(r)
		if !ok {
			return nil
		}
		s, err := l.Summary(r.Context(), uid)
		if err != nil {
			return nil
		}
		return s.Marked
	}
}

// ratings are the user's ratings by titleKey; none without reviews.
func (l *Library) ratings(ctx context.Context, uid string) (map[string]int, error) {
	if l.reviews == nil {
		return nil, nil
	}
	return Ratings(ctx, l.reviews, uid) // from the library list controls
}

// rankOf orders watched titles as seeds: by rating, unrated ones as a 6.
func rankOf(ratings map[string]int, t store.TitleState) int {
	if r := ratings[titleKey(t.Kind, t.TMDBID)]; r > 0 {
		return r
	}
	return 6
}
