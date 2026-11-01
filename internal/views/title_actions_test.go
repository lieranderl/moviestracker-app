package views_test

import (
	"html"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// heroActions is the title hero's action row.
func heroActions(t *testing.T, page string) string {
	t.Helper()
	row := regexp.MustCompile(`(?s)<div class="flex flex-wrap gap-3 pt-1">(.*?)</section>`).FindStringSubmatch(page)
	if row == nil {
		t.Fatal("no hero action row")
	}
	return row[1]
}

// A title page leads with the action that gets watching going: resuming
// when there is something to resume, finding sources otherwise. Trailer,
// watch state, lists and favourites follow, in one secondary style.
func TestTheTitlePageLeadsWithResumeOrFindSources(t *testing.T) {
	pages := map[string]string{
		"movie": html.UnescapeString(render(t, views.MoviePage(testUser, &tmdb.MovieDetails{MediaItem: tmdb.MediaItem{ID: 1, Title: "Dune", MediaType: "movie", TrailerKey: "abc"}}))),
		"tv":    html.UnescapeString(render(t, views.TVPage(testUser, &tmdb.TVDetails{MediaItem: tmdb.MediaItem{ID: 2, Title: "Severance", MediaType: "tv", TrailerKey: "abc"}, Seasons: []tmdb.SeasonSummary{{Number: 1}}}, nil, 1))),
	}
	for kind, page := range pages {
		row := heroActions(t, page)
		progress, sources, trailer := strings.Index(row, `id="progress-button"`), strings.Index(row, `href="#sources"`), strings.Index(row, `data-video="abc"`)
		if progress < 0 || sources < 0 || trailer < 0 || !(progress < sources && sources < trailer) {
			t.Errorf("%s: actions are not Resume, Find sources, then the trailer:\n%s", kind, row)
		}
		find := regexp.MustCompile(`<a href="#sources"[^>]*>`).FindString(row)
		if !strings.Contains(find, "btn-primary") || !strings.Contains(find, `data-class="{'btn-primary': !$_resumable`) {
			t.Errorf("%s: Find sources should be primary until there is something to resume: %s", kind, find)
		}
		if strings.Contains(row, "bg-white text-black") {
			t.Errorf("%s: the trailer still competes as a bright button", kind)
		}
		if !strings.Contains(page, "_resumable: false") {
			t.Errorf("%s: the page does not start with nothing to resume", kind)
		}
	}
}

// Resume, once there is something to resume, is the primary action and
// says so to the page.
func TestResumeIsThePrimaryActionWhenThereIsOne(t *testing.T) {
	out := html.UnescapeString(render(t, views.ProgressButton(&store.Progress{Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 2, Position: time.Minute, Duration: time.Hour})))
	if !strings.Contains(out, "btn-primary") || !strings.Contains(out, `data-init="$_resumable = true"`) {
		t.Errorf("resume button is not primary or does not mark the page resumable:\n%s", out)
	}
	if empty := render(t, views.ProgressButton(nil)); strings.Contains(empty, "_resumable = true") {
		t.Error("with nothing to resume the page is marked resumable")
	}
}
