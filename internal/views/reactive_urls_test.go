package views_test

import (
	"context"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

func TestSharedPagesKeepReactiveURLsUsableAfterRendering(t *testing.T) {
	movie := tmdb.MediaItem{ID: 438631, Title: "Dune", MediaType: "movie", TrailerKey: "abc123"}
	for name, page := range map[string]templ.Component{
		"home with trailer and lazy rows": views.Movies(testUser, []tmdb.MediaItem{movie}, []tmdb.MediaItem{movie}, nil, true, nil),
		"movie trailer":                   views.MoviePage(testUser, &tmdb.MovieDetails{MediaItem: movie}),
		"web player":                      views.WebTorrServerPage(testUser, nil, nil),
	} {
		t.Run(name, func(t *testing.T) {
			for _, cloud := range []bool{false, true} {
				body := renderIn(t, views.WithSite(context.Background(), views.Site{Cloud: cloud}), page)
				if strings.Contains(body, "TemplFailedSanitizationURL") {
					t.Fatalf("rendered %s (cloud=%v) contains a rejected URL expression; Datastar cannot initialize", name, cloud)
				}
			}
		})
	}
	body := torrPage(t, true)
	if strings.Contains(body, "TemplFailedSanitizationURL") {
		t.Fatal("local TorrServer page contains a rejected URL attribute")
	}
	if !strings.Contains(body, `data-copy-link="magnet:?xt=urn:btih:hash1&dn=Movie"`) {
		t.Fatal("the shared Copy magnet control did not retain its magnet URI")
	}
}
