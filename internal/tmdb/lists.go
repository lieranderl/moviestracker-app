package tmdb

import (
	"context"
	"strings"
)

// List is one of TMDB's curated title lists. Only the constants below are
// valid, so callers cannot steer requests to arbitrary API paths.
type List string

// Curated lists shown as discovery rails.
const (
	NowPlayingMovies List = "movie/now_playing"
	PopularMovies    List = "movie/popular"
	TopRatedMovies   List = "movie/top_rated"
	PopularSeries    List = "tv/popular"
	TopRatedSeries   List = "tv/top_rated"
)

// List returns the first page of a curated list, cached for the client TTL.
func (c *Client) List(ctx context.Context, list List) ([]MediaItem, error) {
	return cached(ctx, c, "list/"+string(list), func(ctx context.Context) ([]MediaItem, error) {
		var raw tmdbResponse
		if err := c.getJSON(ctx, "/3/"+string(list), nil, &raw); err != nil {
			return nil, err
		}
		mediaType, _, _ := strings.Cut(string(list), "/")
		return normalizeResults(raw.Results, mediaType, maxRelated), nil
	})
}
