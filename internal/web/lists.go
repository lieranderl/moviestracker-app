package web

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

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
// feedTTL; requests that miss it at once share one read. Failed reads are
// not kept. It holds at most every page of three feeds in two languages, so
// it needs no other bound.
type cachedFeeds struct {
	src     releases.Source
	posters tmdb.PosterProvider // nil keeps the backend's posters
	now     func() time.Time
	reading singleflight.Group

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

func newCachedFeeds(src releases.Source, posters tmdb.PosterProvider, now func() time.Time) *cachedFeeds {
	return &cachedFeeds{src: src, posters: posters, now: now, pages: map[feedPage]cachedPage{}}
}

// Poster lookups for a feed page: a few at a time, each briefly.
const (
	posterLookups      = 4
	posterLookupWithin = 3 * time.Second
)

// localizePosters gives a page's releases their posters in ctx's language.
// The backend keeps each release's Russian poster, as it titles them; a
// poster TMDB does not give in time stays Russian.
func (c *cachedFeeds) localizePosters(ctx context.Context, p tmdb.Page) tmdb.Page {
	if c.posters == nil || i18n.FromContext(ctx) == i18n.Russian {
		return p
	}
	items := slices.Clone(p.Items)
	slots := make(chan struct{}, posterLookups)
	var wg sync.WaitGroup
	for i := range items {
		wg.Go(func() {
			slots <- struct{}{}
			defer func() { <-slots }()
			lookup, cancel := context.WithTimeout(ctx, posterLookupWithin)
			defer cancel()
			if poster, err := c.posters.MoviePoster(lookup, items[i].ID); err == nil && poster != "" {
				items[i].PosterPath = poster
			}
		})
	}
	wg.Wait()
	p.Items = items
	return p
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
	// The shared read is not cut short when the request that started it
	// goes away: the others still wait for it.
	v, err, _ := c.reading.Do(fmt.Sprint(key), func() (any, error) {
		p, err := c.src.Page(context.WithoutCancel(ctx), feed, page)
		if err != nil {
			return tmdb.Page{}, err
		}
		p = c.localizePosters(context.WithoutCancel(ctx), p)
		c.mu.Lock()
		c.pages[key] = cachedPage{page: p, expires: c.now().Add(feedTTL)}
		c.mu.Unlock()
		return p, nil
	})
	if err != nil {
		return tmdb.Page{}, err
	}
	return v.(tmdb.Page), nil
}
