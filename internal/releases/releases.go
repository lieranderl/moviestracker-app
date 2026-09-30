// Package releases reads the movies Moviestracker's backend finds on
// torrent trackers (moviestracker-backend), which it keeps in Firestore:
// the latest releases, and those in HDR10 and in Dolby Vision. The cloud web
// app shows them as home rows and browse pages.
package releases

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

// Feed is one of the backend's release collections.
type Feed string

// The backend's feeds, by their Firestore collection.
const (
	Latest      Feed = "latesttorrentsmovies"
	HDR10       Feed = "hdr10movies"
	DolbyVision Feed = "dvmovies"
)

// PageSize is how many releases a page holds, as a TMDB list page.
const PageSize = 20

// maxPages bounds how deep a feed can be paged.
const maxPages = 50

// Release is a movie the backend found, as it keeps it.
type Release struct {
	ID            int // TMDB's
	Title         string
	OriginalTitle string
	PosterPath    string
	BackdropPath  string
	ReleaseDate   string
	VoteAverage   float64
	VoteCount     int
	FoundAt       time.Time // when the backend last found it on a tracker
}

// Source reads the feeds.
type Source interface {
	// Page is page (from 1) of feed, newest found first, as TMDB list items
	// titled in ctx's language.
	Page(ctx context.Context, feed Feed, page int) (tmdb.Page, error)
}

func checkPage(feed Feed, page int) error {
	switch feed {
	case Latest, HDR10, DolbyVision:
	default:
		return fmt.Errorf("no release feed %q", feed)
	}
	if page < 1 || page > maxPages {
		return fmt.Errorf("page %d is outside 1–%d", page, maxPages)
	}
	return nil
}

// item is r as a TMDB list item. The backend titles releases in Russian, as
// the trackers do; other languages get the original title.
func item(ctx context.Context, r Release) tmdb.MediaItem {
	title := r.Title
	if i18n.FromContext(ctx) != i18n.Russian && strings.TrimSpace(r.OriginalTitle) != "" {
		title = r.OriginalTitle
	}
	return tmdb.MediaItem{
		ID: r.ID, Title: title, PosterPath: r.PosterPath, BackdropPath: r.BackdropPath,
		ReleaseDate: r.ReleaseDate, VoteAverage: r.VoteAverage, VoteCount: r.VoteCount, MediaType: "movie",
	}
}

// totalPages is how many pages n releases fill, within maxPages.
func totalPages(n int) int {
	return min((n+PageSize-1)/PageSize, maxPages)
}

// listPrefix marks the TMDB lists that are feeds (see ListOf).
const listPrefix = "releases/"

// ListOf is feed as a catalog list, so the catalog's rows and browse pages
// can show it; the web app serves such lists from its Source.
func ListOf(feed Feed) tmdb.List { return tmdb.List(listPrefix + string(feed)) }

// FeedOf is the feed a catalog list names, if it names one.
func FeedOf(list tmdb.List) (Feed, bool) {
	feed, ok := strings.CutPrefix(string(list), listPrefix)
	return Feed(feed), ok
}
