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

func TestTonightCardsFitTheirGridColumns(t *testing.T) {
	out := render(t, views.DiscoverPage(&auth.User{Name: "Ann"}, views.DiscoverView{
		Pick: 1, Picks: []tmdb.MediaItem{{ID: 42, MediaType: "movie", Title: "Arrival"}},
	}))
	start := strings.Index(out, `id="tonight-picks"`)
	end := strings.Index(out[start:], "</section>")
	shortlist := out[start : start+end]
	if strings.Contains(shortlist, "w-40") || !strings.Contains(shortlist, "w-full") {
		t.Fatal("tonight cards must fit their grid columns rather than use carousel widths")
	}
}

func TestApplyingChooserFiltersSubmitsPinnedTitlesAndTheirKind(t *testing.T) {
	out := render(t, views.DiscoverPage(&auth.User{Name: "Ann"}, views.DiscoverView{
		Query: tmdb.DiscoverQuery{MediaType: "movie"}, Pick: 1, Pins: []int{42, 43},
	}))
	start := strings.Index(out, `action="/discover"`)
	end := strings.Index(out[start:], "</form>")
	form := out[start : start+end]
	for _, want := range []string{`type="hidden" name="pin" value="42"`, `type="hidden" name="pin" value="43"`, `type="hidden" name="pinType" value="movie"`} {
		if !strings.Contains(form, want) {
			t.Errorf("filter submission loses %s", want)
		}
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
