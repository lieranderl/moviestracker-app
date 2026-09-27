package tmdb

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"
)

// Collection is a series of films (a trilogy, a franchise), in release
// order; films without a release date come last.
type Collection struct {
	ID    int
	Name  string
	Parts []MediaItem
}

// defaultCollectionWait keeps a movie page within its deadline: the
// collection is asked after the movie, and is extra.
const defaultCollectionWait = 2 * time.Second

// collectionWithin is the collection id, or nil when TMDB fails or takes
// longer than the client's collection wait, or ctx ends first. A late one
// is still fetched and cached, for the next visit.
func (c *Client) collectionWithin(ctx context.Context, id int) *Collection {
	done := make(chan *Collection, 1)
	go func() {
		col, err := c.collection(context.WithoutCancel(ctx), id)
		if err != nil {
			col = nil
		}
		done <- col
	}()
	wait := time.NewTimer(c.collectionWait)
	defer wait.Stop()
	select {
	case col := <-done:
		return col
	case <-wait.C:
	case <-ctx.Done():
	}
	return nil
}

// collection returns the films of a collection, cached for the client TTL:
// every film of it asks for the same one.
func (c *Client) collection(ctx context.Context, id int) (*Collection, error) {
	return cached(ctx, c, fmt.Sprintf("collection/%d", id), func(ctx context.Context) (*Collection, error) {
		var raw struct {
			ID    int             `json:"id"`
			Name  string          `json:"name"`
			Parts []rawTMDBResult `json:"parts"`
		}
		if err := c.getJSON(ctx, fmt.Sprintf("/3/collection/%d", id), nil, &raw); err != nil {
			return nil, err
		}
		parts := normalizeResults(raw.Parts, "movie", len(raw.Parts))
		for i := range parts {
			parts[i].MediaType = "movie" // a collection holds films only
		}
		// Unreleased films ("" dates) after released ones, each in date order.
		slices.SortStableFunc(parts, func(a, b MediaItem) int {
			return cmp.Or(
				cmp.Compare(boolRank(a.ReleaseDate == ""), boolRank(b.ReleaseDate == "")),
				cmp.Compare(a.ReleaseDate, b.ReleaseDate),
			)
		})
		return &Collection{ID: raw.ID, Name: raw.Name, Parts: parts}, nil
	})
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}
