package views

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

// EpisodeMarks are a user's watched episodes of one series.
type EpisodeMarks struct {
	Track   bool          // the user can mark episodes here
	Watched map[int][]int // by season
}

func (m EpisodeMarks) watched(season, episode int) bool {
	return slices.Contains(m.Watched[season], episode)
}

type episodeMarksKey struct{}

// WithEpisodeMarks gives ctx's series page the user's watched episodes.
func WithEpisodeMarks(ctx context.Context, m EpisodeMarks) context.Context {
	return context.WithValue(ctx, episodeMarksKey{}, m)
}

func episodeMarksOf(ctx context.Context) EpisodeMarks {
	m, _ := ctx.Value(episodeMarksKey{}).(EpisodeMarks)
	return m
}

// today is the date episodes' air dates are compared with.
func today() string { return time.Now().UTC().Format(time.DateOnly) }

// EpisodeAired says whether e is out.
func EpisodeAired(e tmdb.Episode) bool { return e.AirDate != "" && e.AirDate <= today() }

// seasonAired is how many of the season's episodes are out: those aired
// so far while it airs, all once it began and finished.
func seasonAired(s tmdb.SeasonSummary) int {
	if s.Aired > 0 {
		return s.Aired
	}
	if s.AirDate != "" && s.AirDate <= today() {
		return s.EpisodeCount
	}
	return 0
}

// nextUnwatched is the first aired episode, in season order, the user has
// not marked watched; Specials are left out. any says whether any aired.
func nextUnwatched(t *tmdb.TVDetails, m EpisodeMarks) (season, episode int, ok, any bool) {
	last := t.LastEpisode
	for _, s := range t.SeasonsSoFar() { // the airing season counts only what is out
		if s.Number == 0 || (last.Number > 0 && s.Number > last.Season) {
			continue // Specials, and seasons after the last episode out
		}
		for e := 1; e <= seasonAired(s); e++ {
			any = true
			if !m.watched(s.Number, e) {
				return s.Number, e, true, true
			}
		}
	}
	return 0, 0, false, any
}

// EpisodeURL marks one episode watched or not.
func EpisodeURL(tvID, season, episode int, watched bool) string {
	return fmt.Sprintf("/api/episodes?tv=%d&season=%d&episode=%d&watched=%d", tvID, season, episode, boolInt(watched))
}

// SeasonMarkURL marks a season's aired episodes watched or not.
func SeasonMarkURL(tvID, season int, watched bool) string {
	return fmt.Sprintf("/api/episodes/season?tv=%d&season=%d&watched=%d", tvID, season, boolInt(watched))
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
