package views_test

import (
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/auth"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

func TestHomeHelpsAUserChooseSomethingForTonight(t *testing.T) {
	out := render(t, views.Movies(&auth.User{Name: "Ann"}, nil, nil, nil, true, nil))
	if !strings.Contains(out, "Help me choose") || !strings.Contains(out, `href="/discover?pick=1"`) {
		t.Fatal("home must offer a direct tonight chooser")
	}
}

func TestTonightShowsRuntimeAndKeepsPinnedCandidatesWhenRerolling(t *testing.T) {
	out := render(t, views.DiscoverPage(&auth.User{Name: "Ann"}, views.DiscoverView{
		Query: tmdb.DiscoverQuery{MediaType: "movie", Sort: "popular"},
		Pick:  1, Source: "watchlist", Pins: []int{42},
		Picks:        []tmdb.MediaItem{{ID: 42, MediaType: "movie", Title: "Arrival"}},
		PickRuntimes: map[int]int{42: 116},
	}))
	for _, want := range []string{"116 min", "Unpin Arrival", "source=watchlist", "pin=42", "From my watchlist", `type="hidden" name="pick" value="1"`} {
		if !strings.Contains(out, want) {
			t.Errorf("chooser lacks %q", want)
		}
	}
}
