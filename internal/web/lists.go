package web

import (
	"context"
	"sync"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/releases"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

// feedTTL is how long a page of a release feed is kept: the backend adds
// releases a few times a day, and every home page shows three feeds.
const feedTTL = 5 * time.Minute

// releaseLists is TMDB's details with the backend's release feeds among its
// lists, so the catalog's rows and browse pages show them like TMDB's.
type releaseLists struct {
	tmdb.DetailsProvider
	feeds *cachedFeeds
}

// List implements tmdb.DetailsProvider.
func (l releaseLists) List(ctx context.Context, list tmdb.List) ([]tmdb.MediaItem, error) {
	if feed, ok := releases.FeedOf(list); ok {
		p, err := l.feeds.Page(ctx, feed, 1)
		return p.Items, err
	}
	return l.DetailsProvider.List(ctx, list)
}

// ListPage implements tmdb.DetailsProvider.
func (l releaseLists) ListPage(ctx context.Context, list tmdb.List, page int) (tmdb.Page, error) {
	if feed, ok := releases.FeedOf(list); ok {
		return l.feeds.Page(ctx, feed, page)
	}
	return l.DetailsProvider.ListPage(ctx, list, page)
}

// cachedFeeds keeps each page of the feeds read, in each language, for
// feedTTL. Failed reads are not kept. It holds at most every page of three
// feeds in two languages, so it needs no other bound.
type cachedFeeds struct {
	src releases.Source
	now func() time.Time

	mu    sync.Mutex
	pages map[feedPage]cachedPage
}

type feedPage struct {
	feed releases.Feed
	page int
	lang i18n.Lang
}

type cachedPage struct {
	page    tmdb.Page
	expires time.Time
}

func newCachedFeeds(src releases.Source, now func() time.Time) *cachedFeeds {
	return &cachedFeeds{src: src, now: now, pages: map[feedPage]cachedPage{}}
}

// Page implements releases.Source.
func (c *cachedFeeds) Page(ctx context.Context, feed releases.Feed, page int) (tmdb.Page, error) {
	key := feedPage{feed: feed, page: page, lang: i18n.FromContext(ctx)}
	c.mu.Lock()
	hit, ok := c.pages[key]
	c.mu.Unlock()
	if ok && c.now().Before(hit.expires) {
		return hit.page, nil
	}
	p, err := c.src.Page(ctx, feed, page)
	if err != nil {
		return tmdb.Page{}, err
	}
	c.mu.Lock()
	c.pages[key] = cachedPage{page: p, expires: c.now().Add(feedTTL)}
	c.mu.Unlock()
	return p, nil
}
