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
