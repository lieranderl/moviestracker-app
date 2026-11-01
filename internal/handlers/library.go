package handlers

import (
	"context"
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
	add := func(kind string, id int, title string, watched bool) {
		if !s.Marked[titleKey(kind, id)] && len(s.Seeds) < recommendationSeeds {
			s.Seeds = append(s.Seeds, seed{kind, id, title, watched})
		}
		s.Marked[titleKey(kind, id)] = true
	}
	watched, err := l.titles.TitleStates(ctx, uid, store.Watched)
	if err != nil {
		return LibrarySummary{}, err
	}
	for _, t := range watched {
		add(t.Kind, t.TMDBID, t.Title, true)
	}
	if l.favorites != nil {
		favs, err := l.favorites(ctx, uid)
		if err != nil {
			return LibrarySummary{}, err
		}
		for _, f := range favs {
			add(f.Kind, f.TMDBID, f.Title, false)
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
		add(t.Kind, t.TMDBID, t.Title, false)
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
