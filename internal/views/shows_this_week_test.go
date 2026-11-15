package views_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// The user's own rows come first under the billboard, as one zone: what to
// resume, then recommendations; the chooser and the catalog follow.
func TestHomeShowsTheUsersOwnRowsBeforeTheCatalog(t *testing.T) {
	hero := []tmdb.MediaItem{{ID: 1, Title: "Dune", MediaType: "movie", BackdropPath: "/dune.jpg"}}
	watching := []store.Progress{{Hash: "abc", Title: "Severance", Position: 60e9, Duration: 3000e9}}
	out := html.UnescapeString(render(t, views.Movies(testUser, hero, hero, nil, true, watching)))

	order := []string{"Dune", `id="up-next"`, `id="recommendations"`, "What shall we watch tonight?", `id="home-movies"`}
	at := -1
	for _, want := range order {
		i := strings.Index(out, want)
		if i < at {
			t.Errorf("home has %q out of order %q", want, order)
		}
		at = i
	}
	if out := render(t, views.Movies(nil, hero, hero, nil, true, nil)); strings.Contains(out, "/api/home/shows") {
		t.Error("a visitor who is not signed in is asked for followed shows")
	}
}

// What to resume is one row: unfinished files, then followed series with an
// episode this week, which arrive after the page. The row stays out of sight
// until either has a card.
func TestUpNextIsOneRowOfUnfinishedFilesThenThisWeeksShows(t *testing.T) {
	watching := []store.Progress{{Hash: "abc", Title: "Severance", Position: 60e9, Duration: 3000e9}}
	out := html.UnescapeString(render(t, views.Movies(testUser, nil, nil, nil, true, watching)))

	start := strings.Index(out, `<section id="up-next"`)
	if start < 0 {
		t.Fatal("home has no Up next row")
	}
	row, _, _ := strings.Cut(out[start:], "</section>")
	section := regexp.MustCompile(`<section[^>]*>`).FindString(row)
	if !strings.Contains(section, "hidden") || !strings.Contains(section, "has-[.carousel-item]:block") {
		t.Errorf("Up next shows without a card: %s", section)
	}
	if n := strings.Count(row, `class="carousel `); n != 1 {
		t.Errorf("Up next has %d carousels, want 1", n)
	}
	files, shows := strings.Index(row, `id="continue-watching"`), strings.Index(row, `id="shows-this-week"`)
	if files < 0 || shows < files || !strings.Contains(row, `data-init="@get('/api/home/shows')"`) {
		t.Errorf("Up next lacks the files (%d) then this week's shows (%d):\n%s", files, shows, row)
	}
	for _, want := range []string{"Up next", "Remove Severance from Up next", `href="/calendar"`} {
		if !strings.Contains(row, want) {
			t.Errorf("Up next lacks %q", want)
		}
	}
	for _, gone := range []string{"Continue watching", "Your shows this week"} {
		if strings.Contains(out, gone) {
			t.Errorf("home still has a %q row", gone)
		}
	}
}
