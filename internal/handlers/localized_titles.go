package handlers

import (
	"context"
	"slices"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

// Saved titles are named in the interface language by at most namingLookups
// TMDB lookups at a time, for at most namingWait: a title not named by then
// keeps the name it was saved with.
const (
	namingLookups = 8
	namingWait    = 1500 * time.Millisecond
)

// namedTitle is one title's name in the asked language, by its place.
type namedTitle struct {
	at   int
	item tmdb.MediaItem
}

// LocalizeTitles gives saved titles their names and posters in the
// language ctx carries, from TMDB, by their canonical IDs. A title TMDB
// does not name in time, or at all, keeps what it was saved with.
func (c *Catalog) LocalizeTitles(ctx context.Context, items []tmdb.MediaItem) []tmdb.MediaItem {
	names, ok := c.clients().Details.(tmdb.TitleProvider)
	if !ok || len(items) == 0 {
		return items
	}
	results := make(chan namedTitle, len(items)) // never blocks a lookup that outlives the wait
	slots := make(chan struct{}, namingLookups)
	for i, it := range items {
		go func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			named, err := names.Title(ctx, it.MediaType, it.ID)
			if err != nil {
				named = tmdb.MediaItem{}
			}
			results <- namedTitle{at: i, item: named}
		}()
	}
	out := slices.Clone(items)
	wait := time.NewTimer(namingWait)
	defer wait.Stop()
	for range items {
		select {
		case r := <-results:
			if r.item.Title != "" {
				out[r.at].Title = r.item.Title
			}
			if r.item.PosterPath != "" {
				out[r.at].PosterPath = r.item.PosterPath
			}
		case <-wait.C:
			return out
		}
	}
	return out
}
