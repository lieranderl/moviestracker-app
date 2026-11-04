package views

import (
	"fmt"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

// EpisodeMappingContext identifies the file and the dialog request, never an account.
type EpisodeMappingContext struct {
	Hash    string
	Index   int
	File    string
	TMDBID  int
	Request int64
}

// Key binds a reply to the file and the current dialog request.
func (c EpisodeMappingContext) Key() string {
	return fmt.Sprintf("%s-%d-%d", c.Hash, c.Index, c.Request)
}

// EpisodeMappingView is shared by app and web confirmation responses.
type EpisodeMappingView struct {
	Context        EpisodeMappingContext
	Series         *tmdb.TVDetails
	Season         *tmdb.Season
	Saved          *store.EpisodeMapping
	SelectedSeason int
	First, Last    int
	Problem        string
	Message        string
	Editing        bool
	Focus          bool
	Blocked        bool
	FromFilename   bool
	Stale          bool
}

// episodeRange names one episode (S02E03) or a range in one season (S02E03–E04).
func episodeRange(season, first, last int) string {
	if first == last {
		return fmt.Sprintf("S%02dE%02d", season, first)
	}
	return fmt.Sprintf("S%02dE%02d–E%02d", season, first, last)
}

func episodeLabel(number int, name string) string {
	if name == "" {
		return fmt.Sprintf("E%02d", number)
	}
	return fmt.Sprintf("E%02d · %s", number, name)
}
