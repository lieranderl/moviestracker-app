package sources

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/imdb"
	"github.com/lieranderl/moviestracker-app/internal/jacred"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

// ServiceHealth is how the last call to a service went.
type ServiceHealth struct {
	OK       bool
	Error    string // the last failure, when !OK
	LastCall time.Time
	Latency  time.Duration
	// Calls and Failures count the last hour.
	Calls, Failures int
}

// call is one call's time and outcome, for the hourly counts.
type call struct {
	at time.Time
	ok bool
}

// countWindow is how far back Calls and Failures count.
const countWindow = time.Hour

// HealthOption configures a Health.
type HealthOption func(*Health)

// WithClock overrides the time source.
func WithClock(now func() time.Time) HealthOption {
	return func(h *Health) { h.now = now }
}

// Health records the outcome of real calls to each service; the dashboard
// shows it without polling the services. It is safe for concurrent use.
type Health struct {
	now      func() time.Time
	mu       sync.Mutex
	services map[string]ServiceHealth
	calls    map[string][]call // oldest first, within countWindow
}

// NewHealth returns an empty record.
func NewHealth(opts ...HealthOption) *Health {
	h := &Health{now: time.Now, services: map[string]ServiceHealth{}, calls: map[string][]call{}}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Snapshot returns the record of every service called so far.
func (h *Health) Snapshot() map[string]ServiceHealth {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	out := make(map[string]ServiceHealth, len(h.services))
	for k, v := range h.services {
		h.trim(k, now)
		v.Calls, v.Failures = len(h.calls[k]), 0
		for _, c := range h.calls[k] {
			if !c.ok {
				v.Failures++
			}
		}
		out[k] = v
	}
	return out
}

// record notes a call to service that started at start. A cancelled call
// says nothing about the service; "not found" means the service answered.
func (h *Health) record(service string, start time.Time, err error) {
	if h == nil || errors.Is(err, context.Canceled) {
		return
	}
	s := ServiceHealth{OK: true, LastCall: h.now(), Latency: time.Since(start)}
	if err != nil && !errors.Is(err, tmdb.ErrNotFound) && !errors.Is(err, imdb.ErrNotFound) && !errors.Is(err, imdb.ErrInvalidIMDbID) {
		s.OK, s.Error = false, err.Error()
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.services[service] = s
	h.calls[service] = append(h.calls[service], call{at: s.LastCall, ok: s.OK})
	h.trim(service, s.LastCall)
}

// trim drops calls older than countWindow; the caller holds h.mu.
func (h *Health) trim(service string, now time.Time) {
	calls := h.calls[service]
	cut := 0
	for cut < len(calls) && now.Sub(calls[cut].at) > countWindow {
		cut++
	}
	h.calls[service] = calls[cut:]
}

// tmdbClient is what Moviestracker uses of TMDB.
type tmdbClient interface {
	tmdb.CatalogProvider
	tmdb.DetailsProvider
	tmdb.PosterProvider
}

// watchedTMDB records the health of every TMDB call.
type watchedTMDB struct {
	c tmdbClient
	h *Health
}

func watch[T any](h *Health, service string, call func() (T, error)) (T, error) {
	start := time.Now()
	v, err := call()
	h.record(service, start, err)
	return v, err
}

func (w watchedTMDB) GetCatalog(ctx context.Context) (tmdb.Catalog, error) {
	return watch(w.h, "TMDB", func() (tmdb.Catalog, error) { return w.c.GetCatalog(ctx) })
}

func (w watchedTMDB) Movie(ctx context.Context, id int) (*tmdb.MovieDetails, error) {
	return watch(w.h, "TMDB", func() (*tmdb.MovieDetails, error) { return w.c.Movie(ctx, id) })
}

func (w watchedTMDB) MoviePoster(ctx context.Context, id int) (string, error) {
	return watch(w.h, "TMDB", func() (string, error) { return w.c.MoviePoster(ctx, id) })
}

func (w watchedTMDB) TV(ctx context.Context, id int) (*tmdb.TVDetails, error) {
	return watch(w.h, "TMDB", func() (*tmdb.TVDetails, error) { return w.c.TV(ctx, id) })
}

func (w watchedTMDB) Season(ctx context.Context, tvID, number int) (*tmdb.Season, error) {
	return watch(w.h, "TMDB", func() (*tmdb.Season, error) { return w.c.Season(ctx, tvID, number) })
}

func (w watchedTMDB) Person(ctx context.Context, id int) (*tmdb.Person, error) {
	return watch(w.h, "TMDB", func() (*tmdb.Person, error) { return w.c.Person(ctx, id) })
}

func (w watchedTMDB) Search(ctx context.Context, query string) (*tmdb.SearchResults, error) {
	return watch(w.h, "TMDB", func() (*tmdb.SearchResults, error) { return w.c.Search(ctx, query) })
}

func (w watchedTMDB) List(ctx context.Context, list tmdb.List) ([]tmdb.MediaItem, error) {
	return watch(w.h, "TMDB", func() ([]tmdb.MediaItem, error) { return w.c.List(ctx, list) })
}

func (w watchedTMDB) ListPage(ctx context.Context, list tmdb.List, page int) (tmdb.Page, error) {
	return watch(w.h, "TMDB", func() (tmdb.Page, error) { return w.c.ListPage(ctx, list, page) })
}

// watchedJacRed records the health of every JacRed search.
type watchedJacRed struct {
	c jacred.Searcher
	h *Health
}

func (w watchedJacRed) Search(ctx context.Context, q jacred.Query) ([]jacred.Result, error) {
	return watch(w.h, "JacRed", func() ([]jacred.Result, error) { return w.c.Search(ctx, q) })
}

// watchedIMDb records the health of every rating lookup.
type watchedIMDb struct {
	c imdb.RatingProvider
	h *Health
}

func (w watchedIMDb) GetRating(ctx context.Context, id string) (*imdb.Rating, error) {
	return watch(w.h, "IMDb", func() (*imdb.Rating, error) { return w.c.GetRating(ctx, id) })
}
