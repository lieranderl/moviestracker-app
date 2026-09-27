package tmdb

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
)

const stillImageBase = "https://image.tmdb.org/t/p/w300"

// SeasonSummary is one entry in a series' season list.
type SeasonSummary struct {
	Number       int    `json:"season_number"`
	Name         string `json:"name"`
	EpisodeCount int    `json:"episode_count"`
	AirDate      string `json:"air_date"`
	PosterPath   string `json:"poster_path"`
	Overview     string `json:"overview"`
}

// PosterURL returns the w500 season poster URL or empty string.
func (s SeasonSummary) PosterURL() string { return MediaItem{PosterPath: s.PosterPath}.PosterURL() }

// TVDetails is the full series page model.
type TVDetails struct {
	MediaItem
	OriginalTitle    string
	Tagline          string
	Status           string
	LastAirDate      string
	ContentRating    string
	Homepage         string
	Links            ExternalIDs
	NumberOfSeasons  int
	NumberOfEpisodes int
	EpisodeRuntime   int
	Genres           []Genre
	Creators         []CrewMember
	Networks         []string
	Seasons          []SeasonSummary
	Cast             []CastMember
	Videos           []Video
	Recommendations  []MediaItem
	Similar          []MediaItem
}

// YearRange renders "2008–2013" for ended runs, "2008–" for ongoing ones.
func (t TVDetails) YearRange() string {
	start := t.ReleaseYear()
	if start == "" {
		return ""
	}
	end := ""
	if len(t.LastAirDate) >= 4 {
		end = t.LastAirDate[:4]
	}
	switch {
	case t.Status == "Returning Series" || t.Status == "In Production":
		return start + "–"
	case end == "" || end == start:
		return start
	default:
		return start + "–" + end
	}
}

// FormattedEpisodeRuntime renders the typical episode length.
func (t TVDetails) FormattedEpisodeRuntime() string { return formatMinutes(t.EpisodeRuntime) }

// DefaultSeason is the season opened first: the first regular season.
func (t TVDetails) DefaultSeason() int {
	if len(t.Seasons) == 0 {
		return 1
	}
	return t.Seasons[0].Number
}

// Episode is a single episode within a season.
type Episode struct {
	Number       int     `json:"episode_number"`
	SeasonNumber int     `json:"season_number"`
	Name         string  `json:"name"`
	Overview     string  `json:"overview"`
	AirDate      string  `json:"air_date"`
	Runtime      int     `json:"runtime"`
	StillPath    string  `json:"still_path"`
	VoteAverage  float64 `json:"vote_average"`
}

// Code renders the conventional S01E02 episode code.
func (e Episode) Code() string { return fmt.Sprintf("S%02dE%02d", e.SeasonNumber, e.Number) }

// FormattedRuntime renders the episode length.
func (e Episode) FormattedRuntime() string { return formatMinutes(e.Runtime) }

// StillURL returns the w300 episode still URL or empty string.
func (e Episode) StillURL() string {
	if !validAssetPath(e.StillPath) {
		return ""
	}
	return stillImageBase + e.StillPath
}

// Season is one season with its episodes.
type Season struct {
	ShowID     int
	Number     int       `json:"season_number"`
	Name       string    `json:"name"`
	Overview   string    `json:"overview"`
	AirDate    string    `json:"air_date"`
	PosterPath string    `json:"poster_path"`
	Episodes   []Episode `json:"episodes"`
}

type rawTV struct {
	rawTMDBResult
	OriginalName string   `json:"original_name"`
	Language     string   `json:"original_language"`
	Countries    []string `json:"origin_country"`
	AltTitles    struct {
		Results []altTitle `json:"results"`
	} `json:"alternative_titles"`
	Tagline        string          `json:"tagline"`
	Status         string          `json:"status"`
	LastAirDate    string          `json:"last_air_date"`
	Seasons        []SeasonSummary `json:"seasons"`
	NumberSeasons  int             `json:"number_of_seasons"`
	NumberEpisodes int             `json:"number_of_episodes"`
	EpisodeRunTime []int           `json:"episode_run_time"`
	LastEpisode    struct {
		Runtime int `json:"runtime"`
	} `json:"last_episode_to_air"`
	Genres    []Genre      `json:"genres"`
	CreatedBy []CrewMember `json:"created_by"`
	Networks  []struct {
		Name string `json:"name"`
	} `json:"networks"`
	ContentRatings struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Rating  string `json:"rating"`
		} `json:"results"`
	} `json:"content_ratings"`
	ExternalIDs      ExternalIDs `json:"external_ids"`
	Homepage         string      `json:"homepage"`
	AggregateCredits struct {
		Cast []struct {
			ID          int    `json:"id"`
			Name        string `json:"name"`
			ProfilePath string `json:"profile_path"`
			Roles       []struct {
				Character string `json:"character"`
			} `json:"roles"`
		} `json:"cast"`
	} `json:"aggregate_credits"`
	Videos          struct{ Results []rawVideo } `json:"videos"`
	Images          tmdbImagesResponse           `json:"images"`
	Recommendations tmdbResponse                 `json:"recommendations"`
	Similar         tmdbResponse                 `json:"similar"`
	Translations    translations                 `json:"translations"`
}

// TV returns full details for a series, cached for the client TTL.
func (c *Client) TV(ctx context.Context, id int) (*TVDetails, error) {
	return cached(ctx, c, fmt.Sprintf("tv/%d", id), func(ctx context.Context) (*TVDetails, error) {
		var raw rawTV
		query := detailQuery(ctx, "aggregate_credits,videos,images,content_ratings,external_ids,recommendations,similar,alternative_titles")
		if err := c.getJSON(ctx, fmt.Sprintf("/3/tv/%d", id), query, &raw); err != nil {
			return nil, err
		}
		english := raw.Translations.english()
		raw.Overview = cmp.Or(raw.Overview, english.Overview)
		raw.Tagline = cmp.Or(raw.Tagline, english.Tagline)
		lang := i18n.FromContext(ctx)
		t := &TVDetails{
			MediaItem:        raw.mediaItem("tv"),
			OriginalTitle:    nativeTitle(raw.OriginalName, raw.Language, raw.Countries, raw.AltTitles.Results),
			Tagline:          raw.Tagline,
			Status:           raw.Status,
			LastAirDate:      raw.LastAirDate,
			Homepage:         raw.Homepage,
			Links:            raw.ExternalIDs,
			NumberOfSeasons:  raw.NumberSeasons,
			NumberOfEpisodes: raw.NumberEpisodes,
			Genres:           raw.Genres,
			Creators:         raw.CreatedBy,
			Seasons:          regularSeasonsFirst(raw.Seasons),
			Videos:           youTubeVideos(raw.Videos.Results, lang),
			Recommendations:  relatedTitles(raw.Recommendations.Results, "tv"),
			Similar:          relatedTitles(raw.Similar.Results, "tv"),
		}
		t.ImdbID = raw.ExternalIDs.IMDb
		t.LogoPath = selectLogo(raw.Images.Logos, lang)
		t.TrailerKey = selectTrailer(raw.Videos.Results, lang)
		// episode_run_time is often empty for newer shows; fall back to the
		// most recent episode's runtime.
		t.EpisodeRuntime = raw.LastEpisode.Runtime
		if len(raw.EpisodeRunTime) > 0 {
			t.EpisodeRuntime = raw.EpisodeRunTime[0]
		}
		for _, n := range raw.Networks {
			t.Networks = append(t.Networks, n.Name)
		}
		for _, r := range raw.ContentRatings.Results {
			if r.Country == "US" {
				t.ContentRating = r.Rating
			}
		}
		for _, member := range limit(raw.AggregateCredits.Cast, maxCast) {
			cm := CastMember{ID: member.ID, Name: member.Name, ProfilePath: member.ProfilePath}
			if len(member.Roles) > 0 {
				cm.Character = member.Roles[0].Character
			}
			t.Cast = append(t.Cast, cm)
		}
		return t, nil
	})
}

// regularSeasonsFirst orders seasons 1..n and moves Specials (0) to the end.
func regularSeasonsFirst(seasons []SeasonSummary) []SeasonSummary {
	out := slices.Clone(seasons)
	slices.SortStableFunc(out, func(a, b SeasonSummary) int {
		if (a.Number == 0) != (b.Number == 0) {
			if a.Number == 0 {
				return 1
			}
			return -1
		}
		return a.Number - b.Number
	})
	return out
}

// Season returns one season's episodes, cached for the client TTL.
func (c *Client) Season(ctx context.Context, tvID, number int) (*Season, error) {
	return cached(ctx, c, fmt.Sprintf("tv/%d/season/%d", tvID, number), func(ctx context.Context) (*Season, error) {
		var s Season
		if err := c.getJSON(ctx, fmt.Sprintf("/3/tv/%d/season/%d", tvID, number), nil, &s); err != nil {
			return nil, err
		}
		s.ShowID = tvID
		return &s, nil
	})
}
