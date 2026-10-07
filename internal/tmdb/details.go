package tmdb

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
	"strings"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
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

	lang string
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
	Budget          int64
	Revenue         int64
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
	// Collection is the series of films the movie belongs to; nil when it
	// belongs to none, or TMDB did not send it in time.
	Collection *Collection
	// Releases are when the movie first came out of each kind, anywhere.
	Releases Releases

	collectionID int
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
	Budget       int64                        `json:"budget"`
	Revenue      int64                        `json:"revenue"`
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
				Date          string `json:"release_date"`
			} `json:"release_dates"`
		} `json:"results"`
	} `json:"release_dates"`
	Recommendations tmdbResponse `json:"recommendations"`
	Similar         tmdbResponse `json:"similar"`
	Translations    translations `json:"translations"`
	Collection      *struct {
		ID int `json:"id"`
	} `json:"belongs_to_collection"`
}

// Movie returns full details for a movie, cached for the client TTL, with
// its collection when TMDB sends that in time.
func (c *Client) Movie(ctx context.Context, id int) (*MovieDetails, error) {
	m, err := c.movie(ctx, id)
	if err != nil || m.collectionID == 0 {
		return m, err
	}
	col := c.collectionWithin(ctx, m.collectionID)
	if col == nil || len(col.Parts) < 2 {
		return m, nil
	}
	// Cached details are shared: the collection goes on a copy.
	withCollection := *m
	withCollection.Collection = col
	return &withCollection, nil
}

// movie is a movie's details without its collection.
func (c *Client) movie(ctx context.Context, id int) (*MovieDetails, error) {
	return cached(ctx, c, fmt.Sprintf("movie/%d", id), func(ctx context.Context) (*MovieDetails, error) {
		var raw rawMovie
		query := detailQuery(ctx, "credits,videos,images,release_dates,recommendations,similar,external_ids,alternative_titles")
		if err := c.getJSON(ctx, fmt.Sprintf("/3/movie/%d", id), query, &raw); err != nil {
			return nil, err
		}
		english := raw.Translations.english()
		raw.Overview = cmp.Or(raw.Overview, english.Overview)
		raw.Tagline = cmp.Or(raw.Tagline, english.Tagline)
		lang := i18n.FromContext(ctx)
		m := &MovieDetails{
			MediaItem:       raw.mediaItem("movie"),
			OriginalTitle:   nativeTitle(raw.OriginalTitle, raw.Language, raw.Countries, raw.AltTitles.Titles),
			Tagline:         raw.Tagline,
			Runtime:         raw.Runtime,
			Budget:          raw.Budget,
			Revenue:         raw.Revenue,
			Status:          raw.Status,
			Homepage:        raw.Homepage,
			Links:           raw.ExternalIDs,
			Genres:          raw.Genres,
			Cast:            limit(raw.Credits.Cast, maxCast),
			Directors:       crewWithJobs(raw.Credits.Crew, "Director"),
			Writers:         crewWithJobs(raw.Credits.Crew, "Screenplay", "Writer", "Story", "Novel"),
			Videos:          youTubeVideos(raw.Videos.Results, lang),
			Recommendations: relatedTitles(raw.Recommendations.Results, "movie"),
			Similar:         relatedTitles(raw.Similar.Results, "movie"),
		}
		m.ImdbID = raw.ImdbID
		if raw.Collection != nil {
			m.collectionID = raw.Collection.ID
		}
		m.LogoPath = selectLogo(raw.Images.Logos, lang)
		m.TrailerKey = selectTrailer(raw.Videos.Results, lang)
		for _, country := range raw.ReleaseDates.Results {
			for _, d := range country.Dates {
				m.Releases.add(d.Type, d.Date)
			}
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

// youTubeVideos keeps embeddable YouTube videos, trailers first and, among
// each, those in lang first.
func youTubeVideos(raw []rawVideo, lang i18n.Lang) []Video {
	var trailers, others []Video
	for _, v := range raw {
		if !strings.EqualFold(v.Site, "YouTube") || !validYouTubeKey(v.Key) {
			continue
		}
		video := Video{Key: v.Key, Name: v.Name, Type: v.Type, Official: v.Official, lang: v.Iso6391}
		if v.Type == "Trailer" {
			trailers = append(trailers, video)
		} else {
			others = append(others, video)
		}
	}
	inLangFirst := func(a, b Video) int {
		return cmp.Compare(boolRank(a.lang != string(lang)), boolRank(b.lang != string(lang)))
	}
	slices.SortStableFunc(trailers, inLangFirst)
	slices.SortStableFunc(others, inLangFirst)
	return append(trailers, others...)
}

// detailQuery asks for a title's details with appends, their images and
// videos in the language ctx carries or in English, and, when that language
// is not English, the English translation its blanks fall back to.
func detailQuery(ctx context.Context, appends string) url.Values {
	if i18n.FromContext(ctx) != i18n.English {
		appends += ",translations"
	}
	query := url.Values{"append_to_response": {appends}}
	withMediaLanguages(ctx, query)
	return query
}

// translations are a title's or person's texts in every language TMDB has.
type translations struct {
	Translations []struct {
		Language string `json:"iso_639_1"`
		Data     struct {
			Overview  string `json:"overview"`
			Tagline   string `json:"tagline"`
			Biography string `json:"biography"`
		} `json:"data"`
	} `json:"translations"`
}

// english is the English translation, empty when TMDB has none.
func (t translations) english() (out struct{ Overview, Tagline, Biography string }) {
	for _, tr := range t.Translations {
		if tr.Language == "en" {
			return struct{ Overview, Tagline, Biography string }{tr.Data.Overview, tr.Data.Tagline, tr.Data.Biography}
		}
	}
	return out
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

// Releases are a movie's first release of each kind in any country: when
// releases of that kind start to appear. Zero when TMDB knows of none.
type Releases struct {
	Cinema   time.Time // limited or wide, not a premiere
	Digital  time.Time
	Physical time.Time // DVD, Blu-ray
}

// TMDB's release types.
const (
	releaseLimited  = 2
	releaseCinema   = 3
	releaseDigital  = 4
	releasePhysical = 5
)

// add keeps the release of kind on date when it is that kind's earliest.
func (r *Releases) add(kind int, date string) {
	day, err := time.Parse(time.RFC3339, date)
	if err != nil {
		return
	}
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	var first *time.Time
	switch kind {
	case releaseLimited, releaseCinema:
		first = &r.Cinema
	case releaseDigital:
		first = &r.Digital
	case releasePhysical:
		first = &r.Physical
	default:
		return
	}
	if first.IsZero() || day.Before(*first) {
		*first = day
	}
}
