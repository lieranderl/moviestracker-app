package tmdb

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// DiscoverQuery filters TMDB's whole catalog of movies or series. Zero
// fields filter nothing.
type DiscoverQuery struct {
	MediaType string  // "movie" or "tv"
	Genre     int     // a TMDB genre of MediaType
	Year      int     // released, or first aired, that year
	MinRating float64 // TMDB's average vote, at least
	Sort      string  // "popular" (the default), "rating" or "newest"
}

// minVotes keeps titles rated by a handful of people out of rated results.
const minVotes = 100

func (q DiscoverQuery) kind() string {
	if q.MediaType == "tv" {
		return "tv"
	}
	return "movie"
}

// values are q as TMDB's discover parameters, as of today.
func (q DiscoverQuery) values(today time.Time, page int) url.Values {
	v := url.Values{"page": {strconv.Itoa(page)}, "include_adult": {"false"}}
	date, year := "primary_release_date", "primary_release_year"
	if q.kind() == "tv" {
		date, year = "first_air_date", "first_air_date_year"
	}
	if q.Genre > 0 {
		v.Set("with_genres", strconv.Itoa(q.Genre))
	}
	if q.Year > 0 {
		v.Set(year, strconv.Itoa(q.Year))
	}
	if q.MinRating > 0 {
		v.Set("vote_average.gte", strconv.FormatFloat(q.MinRating, 'f', -1, 64))
		v.Set("vote_count.gte", strconv.Itoa(minVotes))
	}
	switch q.Sort {
	case "rating":
		v.Set("sort_by", "vote_average.desc")
		v.Set("vote_count.gte", strconv.Itoa(minVotes))
	case "newest":
		// Newest out, not announced: nothing dated after today.
		v.Set("sort_by", date+".desc")
		v.Set(date+".lte", today.Format(time.DateOnly))
	default:
		v.Set("sort_by", "popularity.desc")
	}
	return v
}

// Discover returns page (1–MaxPage) of the titles q filters, each page
// cached for the client TTL.
func (c *Client) Discover(ctx context.Context, q DiscoverQuery, page int) (Page, error) {
	if page < 1 || page > MaxPage {
		return Page{}, fmt.Errorf("page %d is outside 1–%d", page, MaxPage)
	}
	query := q.values(c.now(), page)
	return cached(ctx, c, "discover/"+q.kind()+"?"+query.Encode(), func(ctx context.Context) (Page, error) {
		var raw struct {
			tmdbResponse
			TotalPages int `json:"total_pages"`
		}
		if err := c.getJSON(ctx, "/3/discover/"+q.kind(), query, &raw); err != nil {
			return Page{}, err
		}
		return Page{
			Items:      normalizeResults(raw.Results, q.kind(), len(raw.Results)),
			Page:       page,
			TotalPages: min(raw.TotalPages, MaxPage),
		}, nil
	})
}

// Genres are TMDB's genres of mediaType ("movie" or "tv"), in the language
// ctx carries, cached for the client TTL.
func (c *Client) Genres(ctx context.Context, mediaType string) ([]Genre, error) {
	kind := DiscoverQuery{MediaType: mediaType}.kind()
	return cached(ctx, c, "genres/"+kind, func(ctx context.Context) ([]Genre, error) {
		var raw struct {
			Genres []Genre `json:"genres"`
		}
		if err := c.getJSON(ctx, "/3/genre/"+kind+"/list", nil, &raw); err != nil {
			return nil, err
		}
		return raw.Genres, nil
	})
}
