package imdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/format"
)

var (
	// ErrInvalidIMDbID is returned when an IMDb identifier is not properly formatted.
	ErrInvalidIMDbID = errors.New("invalid imdb id")
	// ErrNotFound indicates the rating service has no record for the movie.
	ErrNotFound = errors.New("imdb rating not found")

	imdbIDRegex = regexp.MustCompile(`^tt\d{7,10}$`)
)

// Rating represents the response payload from the IMDb rating service.
type Rating struct {
	ID     string `json:"Id"`
	Rating string `json:"Rating"`
	Votes  string `json:"Votes"`
}

// FormattedVotes returns a compact representation of the vote count (e.g. "2k", "57k", "120k", "1.2M")
// matching TMDB format, or empty string if absent or non-positive.
func (r Rating) FormattedVotes() string {
	return format.FormatVotes(r.Votes)
}

// Title returns the tooltip title for the IMDb rating badge.
func (r Rating) Title() string {
	return format.RatingTitle("IMDb", r.Rating, r.FormattedVotes())
}

// RatingProvider defines the interface for fetching IMDb ratings.
type RatingProvider interface {
	GetRating(ctx context.Context, imdbID string) (*Rating, error)
}

type cacheEntry struct {
	rating    *Rating
	fetchedAt time.Time
}

// Client calls the public IMDb rating service.
type Client struct {
	baseURL    string
	httpClient *http.Client
	cacheTTL   time.Duration

	cacheMu sync.RWMutex
	cache   map[string]cacheEntry
}

// Option configures an IMDb Client.
type Option func(*Client)

// WithHTTPClient overrides the internal HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		if client != nil {
			c.httpClient = client
		}
	}
}

// WithCacheTTL customizes the memory cache duration.
func WithCacheTTL(ttl time.Duration) Option {
	return func(c *Client) {
		if ttl > 0 {
			c.cacheTTL = ttl
		}
	}
}

// NewClient creates a client for the public IMDb rating service at baseURL.
func NewClient(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 5 * time.Second},
		cacheTTL:   1 * time.Hour,
		cache:      make(map[string]cacheEntry),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// GetRating retrieves the IMDb rating and vote count for the given IMDb ID.
func (c *Client) GetRating(ctx context.Context, imdbID string) (*Rating, error) {
	trimmedID := strings.TrimSpace(imdbID)
	if !imdbIDRegex.MatchString(trimmedID) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidIMDbID, imdbID)
	}

	// Check cache
	c.cacheMu.RLock()
	entry, found := c.cache[trimmedID]
	if found && time.Since(entry.fetchedAt) < c.cacheTTL {
		c.cacheMu.RUnlock()
		return entry.rating, nil
	}
	c.cacheMu.RUnlock()

	reqURL := fmt.Sprintf("%s/getimdb?imdb_id=%s", c.baseURL, url.QueryEscape(trimmedID))
	// Errors name the service, not its address: net/http quotes the request
	// URL and the dialed host, which logs and the dashboard would show.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, errors.New("create imdb rating request: invalid service address")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("fetch imdb rating: %w", ctxErr)
		}
		if netErr, ok := errors.AsType[net.Error](err); ok && netErr.Timeout() {
			return nil, errors.New("fetch imdb rating: the rating service timed out")
		}
		return nil, errors.New("fetch imdb rating: the rating service did not answer")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("imdb service returned HTTP status %d", resp.StatusCode)
	}

	var rating Rating
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&rating); err != nil {
		return nil, fmt.Errorf("decode imdb rating response: %w", err)
	}

	// Cache result
	c.cacheMu.Lock()
	c.cache[trimmedID] = cacheEntry{
		rating:    &rating,
		fetchedAt: time.Now(),
	}
	c.cacheMu.Unlock()

	return &rating, nil
}
