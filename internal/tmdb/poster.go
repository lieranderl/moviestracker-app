package tmdb

import (
	"cmp"
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

// TitleProvider gives a movie's or series' name and poster in the language
// ctx carries.
type TitleProvider interface {
	Title(ctx context.Context, kind string, id int) (MediaItem, error)
}

// Title is the name and poster of the movie or series ("movie", "tv") id in
// the language ctx carries, cached for the client TTL. It asks for the
// title alone, without the extras of Movie and TV.
func (c *Client) Title(ctx context.Context, kind string, id int) (MediaItem, error) {
	if kind != "movie" && kind != "tv" {
		return MediaItem{}, fmt.Errorf("title kind %q is not movie or tv", kind)
	}
	return cached(ctx, c, fmt.Sprintf("title/%s/%d", kind, id), func(ctx context.Context) (MediaItem, error) {
		var raw struct {
			Title      string `json:"title"`
			Name       string `json:"name"`
			PosterPath string `json:"poster_path"`
		}
		if err := c.getJSON(ctx, fmt.Sprintf("/3/%s/%d", kind, id), nil, &raw); err != nil {
			return MediaItem{}, err
		}
		return MediaItem{ID: id, MediaType: kind, Title: cmp.Or(raw.Title, raw.Name), PosterPath: raw.PosterPath}, nil
	})
}
