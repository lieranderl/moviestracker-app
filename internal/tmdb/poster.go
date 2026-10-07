package tmdb

import (
	"context"
	"fmt"
)

// PosterProvider gives a movie's poster in the language ctx carries.
type PosterProvider interface {
	MoviePoster(ctx context.Context, id int) (string, error)
}

// MoviePoster is the path of movie id's poster in the language ctx
// carries, cached for the client TTL. It asks for the movie alone, without
// the extras of Movie.
func (c *Client) MoviePoster(ctx context.Context, id int) (string, error) {
	return cached(ctx, c, fmt.Sprintf("poster/movie/%d", id), func(ctx context.Context) (string, error) {
		var raw struct {
			PosterPath string `json:"poster_path"`
		}
		if err := c.getJSON(ctx, fmt.Sprintf("/3/movie/%d", id), nil, &raw); err != nil {
			return "", err
		}
		return raw.PosterPath, nil
	})
}
