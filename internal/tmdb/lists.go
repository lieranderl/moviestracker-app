package tmdb

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// List is one of TMDB's curated title lists. Only the constants below are
// valid, so callers cannot steer requests to arbitrary API paths.
type List string

// Curated lists shown as discovery rails and browse pages.
const (
	TrendingMoviesWeek List = "trending/movie/week"
	TrendingMoviesDay  List = "trending/movie/day"
	TrendingSeriesWeek List = "trending/tv/week"
	TrendingSeriesDay  List = "trending/tv/day"
	NowPlayingMovies   List = "movie/now_playing"
	PopularMovies      List = "movie/popular"
	TopRatedMovies     List = "movie/top_rated"
	PopularSeries      List = "tv/popular"
	TopRatedSeries     List = "tv/top_rated"
)

// mediaType is "movie" or "tv": what the list holds.
func (l List) mediaType() string {
	s := strings.TrimPrefix(string(l), "trending/")
	kind, _, _ := strings.Cut(s, "/")
	return kind
}

// List returns the first page of a curated list, cached for the client TTL.
func (c *Client) List(ctx context.Context, list List) ([]MediaItem, error) {
	return cached(ctx, c, "list/"+string(list), func(ctx context.Context) ([]MediaItem, error) {
		var raw tmdbResponse
		if err := c.getJSON(ctx, "/3/"+string(list), nil, &raw); err != nil {
			return nil, err
		}
		return normalizeResults(raw.Results, list.mediaType(), maxRelated), nil
	})
}

// MaxPage is the last page TMDB serves of any list.
const MaxPage = 500

// Page is one page of a list: TMDB sends 20 titles a page.
type Page struct {
	Items            []MediaItem
	Page, TotalPages int
}

// ListPage returns page (1–MaxPage) of a curated list, each page cached for
// the client TTL.
func (c *Client) ListPage(ctx context.Context, list List, page int) (Page, error) {
	if page < 1 || page > MaxPage {
		return Page{}, fmt.Errorf("page %d is outside 1–%d", page, MaxPage)
	}
	return cached(ctx, c, fmt.Sprintf("list/%s/%d", list, page), func(ctx context.Context) (Page, error) {
		var raw struct {
			tmdbResponse
			TotalPages int `json:"total_pages"`
		}
		if err := c.getJSON(ctx, "/3/"+string(list), url.Values{"page": {strconv.Itoa(page)}}, &raw); err != nil {
			return Page{}, err
		}
		return Page{
			Items:      normalizeResults(raw.Results, list.mediaType(), len(raw.Results)),
			Page:       page,
			TotalPages: min(raw.TotalPages, MaxPage),
		}, nil
	})
}
