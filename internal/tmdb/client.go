package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/lieranderl/moviestracker-app/internal/format"
)

var ErrInvalidTimeWindow = errors.New("invalid trending time window")

const (
	defaultBaseURL  = "https://api.themoviedb.org"
	defaultTimeout  = 5 * time.Second
	defaultCacheTTL = 10 * time.Minute
	// heroSize is how many trending titles the home billboard rotates through.
	heroSize = 7
)

// MediaItem represents a movie or TV show normalized from TMDB.
type MediaItem struct {
	ID           int     `json:"id"`
	Title        string  `json:"title"`
	Overview     string  `json:"overview"`
	PosterPath   string  `json:"poster_path"`
	BackdropPath string  `json:"backdrop_path"`
	LogoPath     string  `json:"logo_path,omitempty"`
	TrailerKey   string  `json:"trailer_key,omitempty"`
	ImdbID       string  `json:"imdb_id,omitempty"`
	ReleaseDate  string  `json:"release_date"`
	VoteAverage  float64 `json:"vote_average"`
	VoteCount    int     `json:"vote_count"`
	MediaType    string  `json:"media_type"`
}

// PosterURL returns the TMDB w500 image URL or empty string.
func (m MediaItem) PosterURL() string {
	if !validAssetPath(m.PosterPath) {
		return ""
	}
	return "https://image.tmdb.org/t/p/w500" + m.PosterPath
}

// BackdropURL returns the TMDB w1280 backdrop image URL or empty string.
func (m MediaItem) BackdropURL() string {
	if !validAssetPath(m.BackdropPath) {
		return ""
	}
	return "https://image.tmdb.org/t/p/w1280" + m.BackdropPath
}

// LogoURL returns the TMDB w500 logo image URL or empty string.
func (m MediaItem) LogoURL() string {
	if !validAssetPath(m.LogoPath) {
		return ""
	}
	return "https://image.tmdb.org/t/p/w500" + m.LogoPath
}

func validAssetPath(value string) bool {
	return strings.HasPrefix(value, "/") &&
		!strings.HasPrefix(value, "//") &&
		!strings.Contains(value, "..")
}

// FormattedRating returns a single-decimal representation or "NR".
func (m MediaItem) FormattedRating() string {
	if m.VoteAverage <= 0 {
		return "NR"
	}
	return fmt.Sprintf("%.1f", m.VoteAverage)
}

// FormattedVoteCount returns a compact string representation of VoteCount
// (e.g. "850", "2k", "12k", "57k", "120k", "1.5M") or empty string if <= 0.
func (m MediaItem) FormattedVoteCount() string {
	return format.FormatVoteCount(m.VoteCount)
}

// TMDBRatingTitle returns the tooltip title for the TMDB rating badge.
func (m MediaItem) TMDBRatingTitle() string {
	return format.RatingTitle("TMDB", m.FormattedRating(), m.FormattedVoteCount())
}

// ReleaseYear returns the 4-digit year from ReleaseDate.
func (m MediaItem) ReleaseYear() string {
	if len(m.ReleaseDate) >= 4 {
		return m.ReleaseDate[:4]
	}
	return ""
}

// Catalog is an immutable snapshot of the media required by the movies page.
type Catalog struct {
	Hero      []MediaItem
	Movies    []MediaItem
	Series    []MediaItem
	FetchedAt time.Time
	Stale     bool
}

// CatalogProvider provides one coherent movies-page snapshot.
type CatalogProvider interface {
	GetCatalog(context.Context) (Catalog, error)
}

// Client interacts with the TMDB API.
type Client struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	cacheMu    sync.RWMutex
	refreshMu  sync.Mutex
	cached     *Catalog
	cacheTTL   time.Duration
	now        func() time.Time
	details    *detailCache
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the default TMDB API base URL.
func WithBaseURL(rawURL string) Option {
	return func(c *Client) {
		c.baseURL = strings.TrimRight(rawURL, "/")
	}
}

// WithHTTPClient overrides the default HTTP client.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

func WithCacheTTL(ttl time.Duration) Option {
	return func(c *Client) {
		if ttl > 0 {
			c.cacheTTL = ttl
		}
	}
}

func WithClock(now func() time.Time) Option {
	return func(c *Client) {
		if now != nil {
			c.now = now
		}
	}
}

// NewClient initializes a TMDB API client.
func NewClient(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:   apiKey,
		baseURL:  defaultBaseURL,
		cacheTTL: defaultCacheTTL,
		now:      time.Now,
		details:  newDetailCache(defaultDetailCacheSize),
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func isTMDBV3Key(key string) bool {
	if len(key) != 32 {
		return false
	}
	for _, ch := range key {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') && (ch < 'A' || ch > 'F') {
			return false
		}
	}
	return true
}

func (c *Client) newRequest(ctx context.Context, endpoint string) (*http.Request, error) {
	targetURL := c.baseURL + endpoint
	isV3 := isTMDBV3Key(c.apiKey)
	if isV3 {
		u, err := url.Parse(targetURL)
		if err != nil {
			return nil, fmt.Errorf("parse tmdb url: %w", err)
		}
		q := u.Query()
		q.Set("api_key", c.apiKey)
		u.RawQuery = q.Encode()
		targetURL = u.String()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create tmdb request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if !isV3 {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	return req, nil
}

// do sends req. Transport errors quote the request URL, which carries a v3
// api_key, so the query is dropped from them before they reach any log.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	resp, err := c.httpClient.Do(req)
	if urlErr, ok := errors.AsType[*url.Error](err); ok {
		redacted := *req.URL
		redacted.RawQuery = ""
		urlErr.URL = redacted.String()
	}
	return resp, err
}

type rawTMDBResult struct {
	ID           int     `json:"id"`
	Title        string  `json:"title"`
	Name         string  `json:"name"`
	Overview     string  `json:"overview"`
	PosterPath   string  `json:"poster_path"`
	BackdropPath string  `json:"backdrop_path"`
	ReleaseDate  string  `json:"release_date"`
	FirstAirDate string  `json:"first_air_date"`
	VoteAverage  float64 `json:"vote_average"`
	VoteCount    int     `json:"vote_count"`
	MediaType    string  `json:"media_type"`
}

type tmdbResponse struct {
	Page    int             `json:"page"`
	Results []rawTMDBResult `json:"results"`
}

type tmdbLogoItem struct {
	FilePath string  `json:"file_path"`
	Iso6391  *string `json:"iso_639_1"`
	Width    int     `json:"width"`
	Height   int     `json:"height"`
}

type tmdbImagesResponse struct {
	ID    int            `json:"id"`
	Logos []tmdbLogoItem `json:"logos"`
}

type tmdbVideoItem struct {
	Name     string `json:"name"`
	Key      string `json:"key"`
	Site     string `json:"site"`
	Type     string `json:"type"`
	Official bool   `json:"official"`
	Iso6391  string `json:"iso_639_1"`
}

type tmdbVideosResponse struct {
	ID      int             `json:"id"`
	Results []tmdbVideoItem `json:"results"`
}

type tmdbExternalIDsResponse struct {
	ImdbID string `json:"imdb_id"`
}

type tmdbDetailsResponse struct {
	Images      tmdbImagesResponse      `json:"images"`
	Videos      tmdbVideosResponse      `json:"videos"`
	ExternalIDs tmdbExternalIDsResponse `json:"external_ids"`
}

// GetTrendingMovies retrieves trending movies for the given window ("day" or "week").
func (c *Client) GetTrendingMovies(ctx context.Context, timeWindow string) ([]MediaItem, error) {
	if timeWindow == "" {
		timeWindow = "week"
	}
	if !validTimeWindow(timeWindow) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidTimeWindow, timeWindow)
	}
	return c.fetchTrending(ctx, "movie", timeWindow)
}

// GetTrendingSeries retrieves trending TV series for the given window ("day" or "week").
func (c *Client) GetTrendingSeries(ctx context.Context, timeWindow string) ([]MediaItem, error) {
	if timeWindow == "" {
		timeWindow = "week"
	}
	if !validTimeWindow(timeWindow) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidTimeWindow, timeWindow)
	}
	return c.fetchTrending(ctx, "tv", timeWindow)
}

func validTimeWindow(value string) bool {
	return value == "day" || value == "week"
}

func validMediaType(value string) bool {
	return value == "movie" || value == "tv"
}

func (c *Client) GetCatalog(ctx context.Context) (Catalog, error) {
	c.cacheMu.RLock()
	if c.cached != nil && c.now().Sub(c.cached.FetchedAt) < c.cacheTTL {
		catalog := cloneCatalog(*c.cached)
		c.cacheMu.RUnlock()
		return catalog, nil
	}
	c.cacheMu.RUnlock()

	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	c.cacheMu.RLock()
	if c.cached != nil && c.now().Sub(c.cached.FetchedAt) < c.cacheTTL {
		catalog := cloneCatalog(*c.cached)
		c.cacheMu.RUnlock()
		return catalog, nil
	}
	c.cacheMu.RUnlock()

	catalog, err := c.fetchCatalog(ctx)
	if err != nil {
		c.cacheMu.RLock()
		if c.cached != nil {
			stale := cloneCatalog(*c.cached)
			c.cacheMu.RUnlock()
			stale.Stale = true
			return stale, nil
		}
		c.cacheMu.RUnlock()
		return Catalog{}, err
	}
	c.cacheMu.Lock()
	stored := cloneCatalog(catalog)
	c.cached = &stored
	c.cacheMu.Unlock()
	return cloneCatalog(catalog), nil
}

// fetchCatalog retrieves the trending lists once and derives hero candidates from them.
func (c *Client) fetchCatalog(ctx context.Context) (Catalog, error) {
	var movies, series []MediaItem
	var errMovies, errSeries error
	var wg sync.WaitGroup
	wg.Go(func() {
		movies, errMovies = c.GetTrendingMovies(ctx, "week")
	})
	wg.Go(func() {
		series, errSeries = c.GetTrendingSeries(ctx, "week")
	})
	wg.Wait()
	if errMovies != nil && errSeries != nil {
		return Catalog{}, errors.Join(errMovies, errSeries)
	}
	hero := selectHeroCandidates(movies, series, heroSize)
	c.enrichHero(ctx, hero)
	return Catalog{
		Hero:      hero,
		Movies:    movies,
		Series:    series,
		FetchedAt: c.now(),
	}, nil
}

func cloneCatalog(c Catalog) Catalog {
	c.Hero = slices.Clone(c.Hero)
	c.Movies = slices.Clone(c.Movies)
	c.Series = slices.Clone(c.Series)
	return c
}

func (c *Client) enrichHero(ctx context.Context, hero []MediaItem) {
	const maxConcurrentDetails = 3
	slots := make(chan struct{}, maxConcurrentDetails)
	var wg sync.WaitGroup
	for i := range hero {
		wg.Go(func() {
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				return
			}
			logo, trailer, imdbID, err := c.fetchDetails(ctx, hero[i].MediaType, hero[i].ID)
			if err == nil {
				hero[i].LogoPath = logo
				hero[i].TrailerKey = trailer
				hero[i].ImdbID = imdbID
			}
		})
	}
	wg.Wait()
}

func (c *Client) fetchDetails(ctx context.Context, mediaType string, id int) (string, string, string, error) {
	if !validMediaType(mediaType) {
		mediaType = "movie"
	}
	query := url.Values{"append_to_response": {"images,videos,external_ids"}}
	endpoint := fmt.Sprintf("/3/%s/%d?%s", mediaType, id, query.Encode())
	req, err := c.newRequest(ctx, endpoint)
	if err != nil {
		return "", "", "", err
	}
	resp, err := c.do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("execute tmdb details request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", "", "", fmt.Errorf("tmdb details api status %d", resp.StatusCode)
	}
	var data tmdbDetailsResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&data); err != nil {
		return "", "", "", fmt.Errorf("decode tmdb details: %w", err)
	}
	return selectLogo(data.Images.Logos), selectTrailer(data.Videos.Results), data.ExternalIDs.ImdbID, nil
}

func selectLogo(logos []tmdbLogoItem) string {
	var fallback string
	for _, logo := range logos {
		if !validAssetPath(logo.FilePath) {
			continue
		}
		if logo.Iso6391 != nil && *logo.Iso6391 == "en" {
			return logo.FilePath
		}
		if fallback == "" {
			fallback = logo.FilePath
		}
	}
	return fallback
}

func selectTrailer(videos []tmdbVideoItem) string {
	var best string
	for _, video := range videos {
		if !strings.EqualFold(video.Site, "YouTube") || !validYouTubeKey(video.Key) {
			continue
		}
		if video.Type == "Trailer" && video.Official && (video.Iso6391 == "en" || video.Iso6391 == "") {
			return video.Key
		}
		if video.Type == "Trailer" && best == "" {
			best = video.Key
			continue
		}
		if (video.Type == "Teaser" || video.Type == "Clip") && best == "" {
			best = video.Key
		}
	}
	return best
}

func validYouTubeKey(key string) bool {
	if len(key) != 11 {
		return false
	}
	for _, r := range key {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

func selectHeroCandidates(movies, series []MediaItem, limit int) []MediaItem {
	candidates := make([]MediaItem, 0, limit)
	maxLen := max(len(movies), len(series))
	for i := 0; i < maxLen && len(candidates) < limit; i++ {
		if i < len(movies) && movies[i].BackdropPath != "" {
			candidates = append(candidates, movies[i])
		}
		if i < len(series) && series[i].BackdropPath != "" && len(candidates) < limit {
			candidates = append(candidates, series[i])
		}
	}
	if len(candidates) == 0 {
		for i := 0; i < len(movies) && len(candidates) < limit; i++ {
			candidates = append(candidates, movies[i])
		}
	}
	return candidates
}

func (c *Client) fetchTrending(ctx context.Context, mediaType, timeWindow string) ([]MediaItem, error) {
	endpoint := fmt.Sprintf("/3/trending/%s/%s", mediaType, timeWindow)

	req, err := c.newRequest(ctx, endpoint)
	if err != nil {
		return nil, err
	}

	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("execute tmdb request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("tmdb api returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var data tmdbResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&data); err != nil {
		return nil, fmt.Errorf("decode tmdb response: %w", err)
	}

	items := make([]MediaItem, 0, len(data.Results))
	for _, raw := range data.Results {
		title := raw.Title
		if title == "" {
			title = raw.Name
		}
		relDate := raw.ReleaseDate
		if relDate == "" {
			relDate = raw.FirstAirDate
		}
		itemType := raw.MediaType
		if !validMediaType(itemType) {
			itemType = mediaType
		}

		items = append(items, MediaItem{
			ID:           raw.ID,
			Title:        title,
			Overview:     raw.Overview,
			PosterPath:   raw.PosterPath,
			BackdropPath: raw.BackdropPath,
			ReleaseDate:  relDate,
			VoteAverage:  raw.VoteAverage,
			VoteCount:    raw.VoteCount,
			MediaType:    itemType,
		})
	}

	return items, nil
}
