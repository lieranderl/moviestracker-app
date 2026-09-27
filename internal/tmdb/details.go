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
)

// ErrNotFound reports that TMDB has no resource with the requested id.
var ErrNotFound = errors.New("tmdb: not found")

const (
	maxCast          = 20
	maxRelated       = 20
	detailBodyLimit  = 4 << 20
	profileImageBase = "https://image.tmdb.org/t/p/w185"
)

// DetailsProvider serves the detail and discovery pages.
type DetailsProvider interface {
	Movie(ctx context.Context, id int) (*MovieDetails, error)
	TV(ctx context.Context, id int) (*TVDetails, error)
	Season(ctx context.Context, tvID, number int) (*Season, error)
	Person(ctx context.Context, id int) (*Person, error)
	Search(ctx context.Context, query string) (*SearchResults, error)
	List(ctx context.Context, list List) ([]MediaItem, error)
	ListPage(ctx context.Context, list List, page int) (Page, error)
}

// ExternalIDs are a title's or person's profiles on other sites, as
// reported by TMDB. Empty fields mean no known profile.
type ExternalIDs struct {
	IMDb      string `json:"imdb_id"`
	Wikidata  string `json:"wikidata_id"`
	Facebook  string `json:"facebook_id"`
	Instagram string `json:"instagram_id"`
	Twitter   string `json:"twitter_id"`
	TikTok    string `json:"tiktok_id"`
	YouTube   string `json:"youtube_id"`
}

// Genre is a TMDB genre label.
type Genre struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// CastMember is an actor credited on a title.
type CastMember struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Character   string `json:"character"`
	ProfilePath string `json:"profile_path"`
}

// ProfileURL returns the w185 headshot URL or empty string.
func (c CastMember) ProfileURL() string { return profileURL(c.ProfilePath) }

// CrewMember is a non-acting credit such as a director or writer.
type CrewMember struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Job         string `json:"job"`
	Department  string `json:"department"`
	ProfilePath string `json:"profile_path"`
}

// Video is a playable YouTube video attached to a title.
type Video struct {
	Key      string
	Name     string
	Type     string
	Official bool
}

// ThumbnailURL returns the YouTube poster frame for the video.
func (v Video) ThumbnailURL() string { return "https://i.ytimg.com/vi/" + v.Key + "/hqdefault.jpg" }

// WatchURL opens the video on YouTube.
func (v Video) WatchURL() string { return "https://www.youtube.com/watch?v=" + v.Key }

// MovieDetails is the full movie page model.
type MovieDetails struct {
	MediaItem
	OriginalTitle   string
	Tagline         string
	Runtime         int
	Status          string
	Certification   string
	Homepage        string
	Links           ExternalIDs
	Genres          []Genre
	Cast            []CastMember
	Directors       []CrewMember
	Writers         []CrewMember
	Videos          []Video
	Recommendations []MediaItem
	Similar         []MediaItem
}

// FormattedRuntime renders minutes as "2h 28m", "45m" or "".
func (m MovieDetails) FormattedRuntime() string { return formatMinutes(m.Runtime) }

func formatMinutes(total int) string {
	if total <= 0 {
		return ""
	}
	h, m := total/60, total%60
	switch {
	case h == 0:
		return fmt.Sprintf("%dm", m)
	case m == 0:
		return fmt.Sprintf("%dh", h)
	default:
		return fmt.Sprintf("%dh %dm", h, m)
	}
}

func profileURL(path string) string {
	if !validAssetPath(path) {
		return ""
	}
	return profileImageBase + path
}

type rawVideo = tmdbVideoItem

type rawCredits struct {
	Cast []CastMember `json:"cast"`
	Crew []CrewMember `json:"crew"`
}

type rawMovie struct {
	rawTMDBResult
	OriginalTitle string   `json:"original_title"`
	Language      string   `json:"original_language"`
	Countries     []string `json:"origin_country"`
	AltTitles     struct {
		Titles []altTitle `json:"titles"`
	} `json:"alternative_titles"`
	Tagline      string                       `json:"tagline"`
	Runtime      int                          `json:"runtime"`
	Status       string                       `json:"status"`
	ImdbID       string                       `json:"imdb_id"`
	Homepage     string                       `json:"homepage"`
	ExternalIDs  ExternalIDs                  `json:"external_ids"`
	Genres       []Genre                      `json:"genres"`
	Credits      rawCredits                   `json:"credits"`
	Videos       struct{ Results []rawVideo } `json:"videos"`
	Images       tmdbImagesResponse           `json:"images"`
	ReleaseDates struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Dates   []struct {
				Certification string `json:"certification"`
				Type          int    `json:"type"`
			} `json:"release_dates"`
		} `json:"results"`
	} `json:"release_dates"`
	Recommendations tmdbResponse `json:"recommendations"`
	Similar         tmdbResponse `json:"similar"`
}

// Movie returns full details for a movie, cached for the client TTL.
func (c *Client) Movie(ctx context.Context, id int) (*MovieDetails, error) {
	return cached(ctx, c, fmt.Sprintf("movie/%d", id), func(ctx context.Context) (*MovieDetails, error) {
		var raw rawMovie
		query := url.Values{
			"append_to_response":     {"credits,videos,images,release_dates,recommendations,similar,external_ids,alternative_titles"},
			"include_image_language": {"en,null"},
		}
		if err := c.getJSON(ctx, fmt.Sprintf("/3/movie/%d", id), query, &raw); err != nil {
			return nil, err
		}
		m := &MovieDetails{
			MediaItem:       raw.mediaItem("movie"),
			OriginalTitle:   nativeTitle(raw.OriginalTitle, raw.Language, raw.Countries, raw.AltTitles.Titles),
			Tagline:         raw.Tagline,
			Runtime:         raw.Runtime,
			Status:          raw.Status,
			Homepage:        raw.Homepage,
			Links:           raw.ExternalIDs,
			Genres:          raw.Genres,
			Cast:            limit(raw.Credits.Cast, maxCast),
			Directors:       crewWithJobs(raw.Credits.Crew, "Director"),
			Writers:         crewWithJobs(raw.Credits.Crew, "Screenplay", "Writer", "Story", "Novel"),
			Videos:          youTubeVideos(raw.Videos.Results),
			Recommendations: relatedTitles(raw.Recommendations.Results, "movie"),
			Similar:         relatedTitles(raw.Similar.Results, "movie"),
		}
		m.ImdbID = raw.ImdbID
		m.LogoPath = selectLogo(raw.Images.Logos)
		m.TrailerKey = selectTrailer(raw.Videos.Results)
		for _, country := range raw.ReleaseDates.Results {
			if country.Country != "US" {
				continue
			}
			for _, d := range country.Dates {
				if d.Certification != "" {
					m.Certification = d.Certification
					break
				}
			}
		}
		return m, nil
	})
}

// mediaItem normalizes the shared list fields of a movie or TV payload.
func (r rawTMDBResult) mediaItem(fallbackType string) MediaItem {
	title := r.Title
	if title == "" {
		title = r.Name
	}
	date := r.ReleaseDate
	if date == "" {
		date = r.FirstAirDate
	}
	mediaType := r.MediaType
	if !validMediaType(mediaType) {
		mediaType = fallbackType
	}
	return MediaItem{
		ID:           r.ID,
		Title:        title,
		Overview:     r.Overview,
		PosterPath:   r.PosterPath,
		BackdropPath: r.BackdropPath,
		ReleaseDate:  date,
		VoteAverage:  r.VoteAverage,
		VoteCount:    r.VoteCount,
		MediaType:    mediaType,
	}
}

// newestFirst orders by release date, newest first, undated titles last.
func newestFirst(a, b string) int {
	if (a == "") != (b == "") {
		if a == "" {
			return 1
		}
		return -1
	}
	return strings.Compare(b, a)
}

// relatedTitles normalizes a recommendations/similar list, newest first.
func relatedTitles(results []rawTMDBResult, fallbackType string) []MediaItem {
	items := normalizeResults(results, fallbackType, maxRelated)
	slices.SortStableFunc(items, func(a, b MediaItem) int { return newestFirst(a.ReleaseDate, b.ReleaseDate) })
	return items
}

func normalizeResults(results []rawTMDBResult, fallbackType string, max int) []MediaItem {
	items := make([]MediaItem, 0, min(len(results), max))
	for _, r := range results {
		if len(items) == max {
			break
		}
		items = append(items, r.mediaItem(fallbackType))
	}
	return items
}

func limit[T any](s []T, n int) []T {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// crewWithJobs returns crew holding any of jobs, one entry per person.
func crewWithJobs(crew []CrewMember, jobs ...string) []CrewMember {
	var out []CrewMember
	seen := make(map[int]bool)
	for _, member := range crew {
		for _, job := range jobs {
			if member.Job == job && !seen[member.ID] {
				seen[member.ID] = true
				out = append(out, member)
			}
		}
	}
	return out
}

// youTubeVideos keeps embeddable YouTube videos, trailers first.
func youTubeVideos(raw []rawVideo) []Video {
	var trailers, others []Video
	for _, v := range raw {
		if !strings.EqualFold(v.Site, "YouTube") || !validYouTubeKey(v.Key) {
			continue
		}
		video := Video{Key: v.Key, Name: v.Name, Type: v.Type, Official: v.Official}
		if v.Type == "Trailer" {
			trailers = append(trailers, video)
		} else {
			others = append(others, video)
		}
	}
	return append(trailers, others...)
}

// getJSON performs an authenticated GET and decodes a bounded JSON body.
func (c *Client) getJSON(ctx context.Context, path string, query url.Values, out any) error {
	endpoint := path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := c.newRequest(ctx, endpoint)
	if err != nil {
		return err
	}
	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("execute tmdb request %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrNotFound, path)
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("tmdb %s returned status %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, detailBodyLimit)).Decode(out); err != nil {
		return fmt.Errorf("decode tmdb %s: %w", path, err)
	}
	return nil
}
