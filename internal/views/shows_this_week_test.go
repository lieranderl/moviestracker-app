package views_test

import (
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

func TestHomeAsksForThisWeeksShowsBelowTheBillboardAndContinueWatching(t *testing.T) {
	hero := []tmdb.MediaItem{{ID: 1, Title: "Dune", MediaType: "movie", BackdropPath: "/dune.jpg"}}
	watching := []store.Progress{{Hash: "abc", Title: "Severance", Position: 60e9, Duration: 3000e9}}
	out := render(t, views.Movies(testUser, hero, hero, nil, true, watching))
	billboard, resume := strings.Index(out, "Dune"), strings.Index(out, `id="continue-watching-title"`)
	slot := strings.Index(out, `id="shows-this-week"`)
	if slot < 0 || !strings.Contains(out, `data-init="@get('/api/home/shows')"`) {
		t.Fatal("home does not ask for this week's shows")
	}
	if billboard < 0 || resume < 0 || billboard > slot || resume > slot {
		t.Errorf("order: billboard %d, continue watching %d, shows this week %d", billboard, resume, slot)
	}
	if out := render(t, views.Movies(nil, hero, hero, nil, true, nil)); strings.Contains(out, "/api/home/shows") {
		t.Error("a visitor who is not signed in is asked for followed shows")
	}
}
