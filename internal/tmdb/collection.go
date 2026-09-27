package tmdb

import (
	"cmp"
	"context"
	"fmt"
	"slices"
)

// Collection is a series of films (a trilogy, a franchise), in release
// order; films without a release date come last.
type Collection struct {
	ID    int
	Name  string
	Parts []MediaItem
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
