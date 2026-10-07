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
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultBaseURL is the public JacRed instance.
	DefaultBaseURL = "https://jacred.su"

	// searchAPIURL is where jacred.su answers searches since 2026: its
	// search API, which from 9 Oct 2026 needs a key.
	searchAPIURL = "https://api.jacred.su"

	// defaultCacheTTL is long because a personal jacred.su key allows only
	// 100 searches a day.
	defaultTimeout  = 15 * time.Second
	defaultCacheTTL = time.Hour
	maxCacheEntries = 128
	maxResults      = 100
	responseBodyCap = 8 << 20
	yearTolerance   = 1
	magnetPrefix    = "magnet:?"
	infoHashMarker  = "xt=urn:btih:"
	searchPath      = "/api/v1.0/torrents"
	apiSearchPath   = "/api/search"
	// apiPageSize is the most results the search API returns at once.
	apiPageSize = 120
)

var (
	// ErrKeyNeeded is JacRed refusing a search without a key, or with a
	// wrong or revoked one.
	ErrKeyNeeded = errors.New("JacRed needs a valid key")
	// ErrBlocked is JacRed refusing the account (or its project).
	ErrBlocked = errors.New("JacRed blocked this key's account")
)

// LimitError is JacRed refusing more searches for now: the key's daily
// quota is used up, or it searched too often. Retry is how long to wait
// (0 when JacRed did not say).
type LimitError struct{ Retry time.Duration }

func (e *LimitError) Error() string {
	if e.Retry > 0 {
		return fmt.Sprintf("JacRed's search limit is reached; try again in %s", e.Retry.Round(time.Minute))
	}
	return "JacRed's search limit is reached; try again later"
}

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
	// Qualities, when set, keeps releases of these qualities only: 2160,
	// 1080, 720 or 480 (SD).
	Qualities []int
	// HDR, when set, keeps HDR releases only.
	HDR bool
}

// qualityOf is the quality a release is filed under: 2160, 1080, 720 or
// 480 (SD), as JacRed's quality filter names them.
func qualityOf(q int) int {
	switch {
	case q >= 2160:
		return 2160
	case q >= 1080:
		return 1080
	case q >= 720:
		return 720
	default:
		return 480
	}
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
	// Runtime is how long the release plays, from TMDB: zero when unknown.
	Runtime time.Duration
}

// Episode markers in release titles: "S02E05", "S02E01-E03" or "S02E07-08",
// and Russian trackers' "1-8 из 10" (episodes 1 to 8 of 10).
var (
	episodeMarker = regexp.MustCompile(`(?i)\bS\d{1,2}E(\d{1,3})(?:\s*-\s*E?(\d{1,3}))?\b`)
	episodesOfAll = regexp.MustCompile(`(?i)\b(\d{1,3})\s*[-–]\s*(\d{1,3})\s*(?:серии|серия|эпизоды)?\s*из\s*\d{1,3}\b`)
	// Several seasons: "S01-S02", "S01-02", "Seasons 1-2", "Сезоны: 1-2".
	// (Go's \b is ASCII-only, so the Russian words go without it.)
	seasonSpan = regexp.MustCompile(`(?i)(?:\bS(\d{1,2})\s*[-–]\s*S?(\d{1,2})\b|(?:\bseasons?|сезон[ыа]?)\s*:?\s*(\d{1,2})\s*[-–]\s*(\d{1,2}))`)
)

// Episodes is how many episodes of one season the release holds, given the
// season's episode count: a range or single episode its title names, else
// the whole season. Zero when it spans several seasons, by JacRed's seasons
// or, when JacRed leaves them out, by its title.
func (r Result) Episodes(seasonEpisodes int) int {
	if len(r.Seasons) > 1 || spansSeasons(r.Title) {
		return 0
	}
	if m := episodeMarker.FindStringSubmatch(r.Title); m != nil {
		return episodeSpan(m[1], m[2])
	}
	if m := episodesOfAll.FindStringSubmatch(r.Title); m != nil {
		return episodeSpan(m[1], m[2])
	}
	return seasonEpisodes
}

// spansSeasons is whether a title names a range of several seasons.
func spansSeasons(title string) bool {
	for _, m := range seasonSpan.FindAllStringSubmatch(title, -1) {
		first, last := m[1], m[2]
		if first == "" {
			first, last = m[3], m[4]
		}
		a, _ := strconv.Atoi(first)
		b, _ := strconv.Atoi(last)
		if b > a {
			return true
		}
	}
	return false
}

// episodeSpan counts the episodes from first to last ("" for just first).
func episodeSpan(first, last string) int {
	a, _ := strconv.Atoi(first)
	b, err := strconv.Atoi(last)
	if err != nil || b < a {
		return 1
	}
	return b - a + 1
}

// Plausible bitrates, in Mbps: outside them the size and runtime are not
// of the same thing (a season pack timed as one episode, a sample).
const minMbps, maxMbps = 0.3, 150

// Mbps estimates the release's bitrate, audio included, as its size over
// its runtime; zero when either is unknown or the two cannot belong together.
func (r Result) Mbps() float64 {
	if r.Size <= 0 || r.Runtime <= 0 {
		return 0
	}
	mbps := float64(r.Size) * 8 / 1e6 / r.Runtime.Seconds()
	if mbps < minMbps || mbps > maxMbps {
		return 0
	}
	return mbps
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
	baseURL string
	apiKey  string
	// searchAPI is the search API's protocol (jacred.su); otherwise the
	// jacred-fdb one, which self-hosted instances run.
	searchAPI  bool
	httpClient *http.Client
	cacheTTL   time.Duration
	now        func() time.Time

	mu    sync.Mutex
	cache map[string]cacheEntry
	quota quota
}

// quota is what the search API last said about the key's daily searches.
type quota struct {
	known     bool
	remaining int
	reset     time.Time
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

// WithAPIKey sets the key: a Bearer header for the search API, the apikey
// parameter for private jacred-fdb instances.
func WithAPIKey(key string) Option { return func(c *Client) { c.apiKey = key } }

// WithSearchAPI speaks the search API's protocol (api.jacred.su's) to the
// base URL, whatever its host.
func WithSearchAPI() Option { return func(c *Client) { c.searchAPI = true } }

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
	baseURL = strings.TrimRight(baseURL, "/")
	searchAPI := false
	if u, err := url.Parse(baseURL); err == nil && (u.Host == "jacred.su" || u.Host == "api.jacred.su") {
		baseURL, searchAPI = searchAPIURL, true
	}
	c := &Client{
		baseURL:    baseURL,
		searchAPI:  searchAPI,
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

// apiResult is one release as the search API names its fields.
type apiResult struct {
	Tracker   string   `json:"tracker"`
	SourceURL string   `json:"source_url"`
	Title     string   `json:"title"`
	Size      int64    `json:"size"`
	SizeName  string   `json:"size_name"`
	CreatedAt string   `json:"created_at"`
	Seeders   int      `json:"seeders"`
	Peers     int      `json:"peers"`
	Magnet    string   `json:"magnet"`
	Year      int      `json:"year"`
	VideoType string   `json:"video_type"`
	Quality   int      `json:"quality"`
	Voices    []string `json:"voices"`
	Seasons   []int    `json:"seasons"`
}

func (a apiResult) raw() rawResult {
	return rawResult{
		Tracker: a.Tracker, URL: a.SourceURL, Title: a.Title, Size: a.Size, SizeName: a.SizeName,
		CreateTime: a.CreatedAt, Seeders: a.Seeders, Peers: a.Peers, Magnet: a.Magnet, Released: a.Year,
		VideoType: a.VideoType, Quality: a.Quality, Voices: a.Voices, Seasons: a.Seasons,
	}
}

// Search returns playable releases, most seeded first. Results are cached
// briefly and must be treated as read-only.
func (c *Client) Search(ctx context.Context, q Query) ([]Result, error) {
	term := q.primary()
	if term == "" {
		return nil, nil
	}
	key := strings.ToLower(fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%d\x00%v\x00%t", term, strings.TrimSpace(q.Title), q.Year, q.Season, q.SeasonYear, q.Qualities, q.HDR))
	if results, ok := c.cached(key); ok {
		return results, nil
	}

	req, err := c.request(ctx, term, q)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		// The quoted request URL can carry the apikey; keep it out of logs.
		if urlErr, ok := errors.AsType[*url.Error](err); ok {
			urlErr.URL = c.baseURL + req.URL.Path
		}
		return nil, fmt.Errorf("execute jacred request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	c.noteQuota(resp.Header)
	if err := refusal(resp); err != nil {
		return nil, err
	}
	raw, err := c.decode(io.LimitReader(resp.Body, responseBodyCap))
	if err != nil {
		return nil, fmt.Errorf("decode jacred response: %w", err)
	}

	results := normalize(raw, q)
	c.store(key, results)
	return results, nil
}

// request asks for term in the instance's protocol.
func (c *Client) request(ctx context.Context, term string, q Query) (*http.Request, error) {
	var target string
	if c.searchAPI {
		params := url.Values{"query": {term}, "limit": {strconv.Itoa(apiPageSize)}}
		if q.Season > 0 {
			params.Set("season", strconv.Itoa(q.Season))
		}
		// The API filters one quality; several are filtered here instead.
		if len(q.Qualities) == 1 {
			params.Set("quality", strconv.Itoa(q.Qualities[0]))
		}
		if q.HDR {
			params.Set("videotype", "hdr")
		}
		target = c.baseURL + apiSearchPath + "?" + params.Encode()
	} else {
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
		target = c.baseURL + searchPath + "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, fmt.Errorf("create jacred request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.searchAPI && c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	return req, nil
}

// decode reads either protocol's results.
func (c *Client) decode(body io.Reader) ([]rawResult, error) {
	if !c.searchAPI {
		var raw []rawResult
		err := json.NewDecoder(body).Decode(&raw)
		return raw, err
	}
	var page struct {
		Results []apiResult `json:"results"`
	}
	if err := json.NewDecoder(body).Decode(&page); err != nil {
		return nil, err
	}
	raw := make([]rawResult, len(page.Results))
	for i, r := range page.Results {
		raw[i] = r.raw()
	}
	return raw, nil
}

// refusal turns JacRed's refusals into errors that say what to do.
func refusal(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized:
		return ErrKeyNeeded
	case http.StatusForbidden:
		return ErrBlocked
	case http.StatusTooManyRequests:
		secs, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		return &LimitError{Retry: time.Duration(max(secs, 0)) * time.Second}
	default:
		return fmt.Errorf("jacred returned status %d", resp.StatusCode)
	}
}

// noteQuota remembers the key's remaining daily searches, when JacRed says.
func (c *Client) noteQuota(h http.Header) {
	left, err := strconv.Atoi(h.Get("X-RateLimit-Remaining"))
	if err != nil {
		return
	}
	q := quota{known: true, remaining: left}
	if reset, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		q.reset = time.Unix(reset, 0)
	}
	c.mu.Lock()
	c.quota = q
	c.mu.Unlock()
}

// Quota is how many searches the key has left today and when they renew,
// as JacRed last said; ok is false until it has said.
func (c *Client) Quota() (remaining int, reset time.Time, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.quota.remaining, c.quota.reset, c.quota.known
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
		} else if t, err := time.Parse(time.DateOnly, r.CreateTime); err == nil {
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

// matches reports whether a release belongs to the queried title (and
// season), in the qualities and video type asked for.
func (q Query) matches(r rawResult) bool {
	if q.HDR && !strings.EqualFold(r.VideoType, "hdr") {
		return false
	}
	if len(q.Qualities) > 0 && !slices.Contains(q.Qualities, qualityOf(r.Quality)) {
		return false
	}
	return q.belongs(r)
}

// belongs reports whether a release is of the queried title (and season).
func (q Query) belongs(r rawResult) bool {
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
// (newest first), "size" (largest first) or "bitrate" (highest first, the
// releases without one last).
func Sort(results []Result, by string) []Result {
	out := slices.Clone(results)
	slices.SortStableFunc(out, func(a, b Result) int {
		switch by {
		case "date":
			return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.Seeders, a.Seeders))
		case "size":
			return cmp.Or(cmp.Compare(b.Size, a.Size), cmp.Compare(b.Seeders, a.Seeders))
		case "bitrate":
			return cmp.Or(cmp.Compare(b.Mbps(), a.Mbps()), cmp.Compare(b.Seeders, a.Seeders))
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
