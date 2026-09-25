// Package jacred searches a JacRed torrent index for releases of a title.
package jacred

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultBaseURL is the public JacRed instance.
	DefaultBaseURL = "https://jacred.su"

	defaultTimeout  = 15 * time.Second
	defaultCacheTTL = 5 * time.Minute
	maxCacheEntries = 128
	maxResults      = 100
	responseBodyCap = 8 << 20
	yearTolerance   = 1
	magnetPrefix    = "magnet:?"
	infoHashMarker  = "xt=urn:btih:"
	searchPath      = "/api/v1.0/torrents"
)

// Query describes the title to look up.
type Query struct {
	// Title is the localized title; sent as an alternative name.
	Title string
	// OriginalTitle is the primary search term.
	OriginalTitle string
	// Year, when set, drops releases of other titles with the same name. For
	// a series it is the first-air year.
	Year int
	// Season, when set, restricts a series search to releases containing it.
	Season int
	// SeasonYear is the season's own air year; season packs are often dated
	// by it rather than by the show's first year.
	SeasonYear int
}

func (q Query) primary() string {
	if s := strings.TrimSpace(q.OriginalTitle); s != "" {
		return s
	}
	return strings.TrimSpace(q.Title)
}

// Result is one torrent release.
type Result struct {
	Tracker   string
	Title     string
	SourceURL string
	Magnet    string
	Size      int64
	SizeName  string
	Seeders   int
	Peers     int
	Quality   int
	HDR       bool
	Year      int
	Voices    []string
	Seasons   []int
	CreatedAt time.Time
}

// QualityLabel renders the vertical resolution as a familiar label.
func (r Result) QualityLabel() string {
	switch {
	case r.Quality >= 2160:
		return "4K"
	case r.Quality >= 1080:
		return "1080p"
	case r.Quality >= 720:
		return "720p"
	default:
		return "SD"
	}
}

// Client queries a JacRed instance.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	cacheTTL   time.Duration
	now        func() time.Time

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	results []Result
	expires time.Time
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient overrides the default HTTP client.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithAPIKey sets the apikey parameter required by private instances.
func WithAPIKey(key string) Option { return func(c *Client) { c.apiKey = key } }

// WithCacheTTL overrides how long results are reused.
func WithCacheTTL(ttl time.Duration) Option {
	return func(c *Client) {
		if ttl > 0 {
			c.cacheTTL = ttl
		}
	}
}

// WithClock injects the time source used for cache expiry.
func WithClock(now func() time.Time) Option {
	return func(c *Client) {
		if now != nil {
			c.now = now
		}
	}
}

// NewClient returns a client for the JacRed instance at baseURL.
func NewClient(baseURL string, opts ...Option) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: defaultTimeout},
		cacheTTL:   defaultCacheTTL,
		now:        time.Now,
		cache:      make(map[string]cacheEntry),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Searcher finds torrent releases for a title.
type Searcher interface {
	Search(ctx context.Context, q Query) ([]Result, error)
}

type rawResult struct {
	Tracker    string   `json:"tracker"`
	URL        string   `json:"url"`
	Title      string   `json:"title"`
	Size       int64    `json:"size"`
	SizeName   string   `json:"sizeName"`
	CreateTime string   `json:"createTime"`
	Seeders    int      `json:"sid"`
	Peers      int      `json:"pir"`
	Magnet     string   `json:"magnet"`
	Released   int      `json:"relased"` // sic: JacRed's field name
	VideoType  string   `json:"videotype"`
	Quality    int      `json:"quality"`
	Voices     []string `json:"voices"`
	Seasons    []int    `json:"seasons"`
}

// Search returns playable releases, most seeded first. Results are cached
// briefly and must be treated as read-only.
func (c *Client) Search(ctx context.Context, q Query) ([]Result, error) {
	term := q.primary()
	if term == "" {
		return nil, nil
	}
	key := strings.ToLower(fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%d", term, strings.TrimSpace(q.Title), q.Year, q.Season, q.SeasonYear))
	if results, ok := c.cached(key); ok {
		return results, nil
	}

	params := url.Values{"search": {term}}
	if alt := strings.TrimSpace(q.Title); alt != "" && !strings.EqualFold(alt, term) {
		params.Set("altname", alt)
	}
	if q.Season > 0 {
		params.Set("season", strconv.Itoa(q.Season))
	}
	if c.apiKey != "" {
		params.Set("apikey", c.apiKey)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+searchPath+"?"+params.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("create jacred request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		// The quoted request URL carries the apikey; keep it out of logs.
		if urlErr, ok := errors.AsType[*url.Error](err); ok {
			urlErr.URL = c.baseURL + searchPath
		}
		return nil, fmt.Errorf("execute jacred request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("jacred returned status %d", resp.StatusCode)
	}
	var raw []rawResult
	if err := json.NewDecoder(io.LimitReader(resp.Body, responseBodyCap)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode jacred response: %w", err)
	}

	results := normalize(raw, q)
	c.store(key, results)
	return results, nil
}

// normalize drops unsafe or unrelated releases, dedupes by info hash keeping
// the best-seeded copy, and orders by seeders.
func normalize(raw []rawResult, q Query) []Result {
	best := make(map[string]Result)
	for _, r := range raw {
		hash := infoHash(r.Magnet)
		if hash == "" || !safeSourceURL(r.URL) || !q.matches(r) {
			continue
		}
		res := Result{
			Tracker:   r.Tracker,
			Title:     r.Title,
			SourceURL: r.URL,
			Magnet:    r.Magnet,
			Size:      r.Size,
			SizeName:  r.SizeName,
			Seeders:   r.Seeders,
			Peers:     r.Peers,
			Quality:   r.Quality,
			HDR:       strings.EqualFold(r.VideoType, "hdr"),
			Year:      r.Released,
			Voices:    r.Voices,
			Seasons:   r.Seasons,
		}
		if t, err := time.Parse(time.RFC3339Nano, r.CreateTime); err == nil {
			res.CreatedAt = t
		}
		if prev, ok := best[hash]; !ok || res.Seeders > prev.Seeders {
			best[hash] = res
		}
	}
	results := make([]Result, 0, len(best))
	for _, r := range best {
		results = append(results, r)
	}
	slices.SortFunc(results, func(a, b Result) int {
		return cmp.Or(cmp.Compare(b.Seeders, a.Seeders), cmp.Compare(b.Quality, a.Quality), strings.Compare(a.Title, b.Title))
	})
	if len(results) > maxResults {
		results = results[:maxResults]
	}
	return results
}

func infoHash(magnet string) string {
	if !strings.HasPrefix(magnet, magnetPrefix) {
		return ""
	}
	_, rest, ok := strings.Cut(magnet, infoHashMarker)
	if !ok {
		return ""
	}
	hash, _, _ := strings.Cut(rest, "&")
	return strings.ToLower(hash)
}

func safeSourceURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

// matches reports whether a release belongs to the queried title (and season).
func (q Query) matches(r rawResult) bool {
	if q.Season > 0 {
		// Defense in depth: instances that ignore ?season= still get filtered.
		if len(r.Seasons) > 0 && !slices.Contains(r.Seasons, q.Season) {
			return false
		}
		// Single-episode uploads carry no year; the season filter vouches for them.
		if r.Released == 0 {
			return true
		}
		return nearYear(r.Released, q.Year) || nearYear(r.Released, q.SeasonYear)
	}
	if q.Year == 0 {
		return true
	}
	if r.Released == 0 {
		return strings.Contains(r.Title, strconv.Itoa(q.Year))
	}
	return nearYear(r.Released, q.Year)
}

func nearYear(released, year int) bool {
	return year > 0 && released >= year-yearTolerance && released <= year+yearTolerance
}

// Sort returns a copy of results ordered by "seeders" (default), "date"
// (newest first) or "size" (largest first).
func Sort(results []Result, by string) []Result {
	out := slices.Clone(results)
	slices.SortStableFunc(out, func(a, b Result) int {
		switch by {
		case "date":
			return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.Seeders, a.Seeders))
		case "size":
			return cmp.Or(cmp.Compare(b.Size, a.Size), cmp.Compare(b.Seeders, a.Seeders))
		default:
			return cmp.Or(cmp.Compare(b.Seeders, a.Seeders), cmp.Compare(b.Quality, a.Quality))
		}
	})
	return out
}

func (c *Client) cached(key string) ([]Result, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.cache[key]
	if !ok || !c.now().Before(e.expires) {
		return nil, false
	}
	return e.results, true
}

func (c *Client) store(key string, results []Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if len(c.cache) >= maxCacheEntries {
		for k, e := range c.cache {
			if !now.Before(e.expires) {
				delete(c.cache, k)
			}
		}
		for k := range c.cache {
			if len(c.cache) < maxCacheEntries {
				break
			}
			delete(c.cache, k)
		}
	}
	c.cache[key] = cacheEntry{results: results, expires: now.Add(c.cacheTTL)}
}
