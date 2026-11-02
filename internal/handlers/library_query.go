package handlers

import (
	"cmp"
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// ParseLibraryQuery is how the request's library list is to be searched,
// narrowed and sorted (?q=, ?kind=movie|tv, ?sort=title|rating); values
// it does not know are ignored.
func ParseLibraryQuery(r *http.Request) views.LibraryQuery {
	q := r.URL.Query()
	out := views.LibraryQuery{Q: strings.TrimSpace(q.Get("q"))}
	if len(out.Q) > 100 {
		out.Q = out.Q[:100]
	}
	if k := q.Get("kind"); k == "movie" || k == "tv" {
		out.Kind = k
	}
	if s := q.Get("sort"); s == "title" || s == "rating" {
		out.Sort = s
	}
	return out
}

// LibraryEntry is one title of a library list and when it was added to it.
type LibraryEntry struct {
	Item tmdb.MediaItem
	At   time.Time
}

// ApplyLibraryQuery is the entries q keeps, in its order, and q with their
// count. Ratings are the user's, by "kind-id": by rating, the highest come
// first and unrated titles last; ties keep the most recent first.
func ApplyLibraryQuery(entries []LibraryEntry, q views.LibraryQuery, ratings map[string]int) ([]tmdb.MediaItem, views.LibraryQuery) {
	needle := strings.ToLower(q.Q)
	kept := slices.DeleteFunc(slices.Clone(entries), func(e LibraryEntry) bool {
		return (q.Kind != "" && e.Item.MediaType != q.Kind) || !strings.Contains(strings.ToLower(e.Item.Title), needle)
	})
	recent := func(a, b LibraryEntry) int { return b.At.Compare(a.At) }
	switch q.Sort {
	case "title":
		slices.SortStableFunc(kept, func(a, b LibraryEntry) int {
			return cmp.Or(strings.Compare(strings.ToLower(a.Item.Title), strings.ToLower(b.Item.Title)), recent(a, b))
		})
	case "rating":
		slices.SortStableFunc(kept, func(a, b LibraryEntry) int {
			return cmp.Or(cmp.Compare(ratings[titleKey(b.Item.MediaType, b.Item.ID)], ratings[titleKey(a.Item.MediaType, a.Item.ID)]), recent(a, b))
		})
	default:
		slices.SortStableFunc(kept, recent)
	}
	items := make([]tmdb.MediaItem, len(kept))
	for i, e := range kept {
		items[i] = e.Item
	}
	q.Count = len(items)
	return items, q
}

// Ratings are the user's ratings by "kind-id", for sorting by rating; none
// without reviews.
func Ratings(ctx context.Context, reviews store.ReviewStore, uid string) map[string]int {
	out := map[string]int{}
	if reviews == nil {
		return out
	}
	all, err := reviews.Reviews(ctx, uid)
	if err != nil {
		return out
	}
	for _, r := range all {
		if r.Rating > 0 {
			out[titleKey(r.Kind, r.TMDBID)] = r.Rating
		}
	}
	return out
}
