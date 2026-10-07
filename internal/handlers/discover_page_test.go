package handlers

import (
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

func discoverDetails() *fakeTMDBDetails {
	return &fakeTMDBDetails{
		genres: map[string][]tmdb.Genre{
			"movie": {{ID: 28, Name: "Action"}, {ID: 878, Name: "Science Fiction"}},
			"tv":    {{ID: 10765, Name: "Sci-Fi & Fantasy"}},
		},
		discovered: tmdb.Page{TotalPages: 3, Items: []tmdb.MediaItem{{ID: 27205, Title: "Inception", MediaType: "movie", PosterPath: "/p.jpg"}}},
	}
}

func TestDiscoverFiltersTMDBsWholeCatalog(t *testing.T) {
	details := discoverDetails()
	server := newMediaServer(t, details, &fakeJacRed{}, "")

	body := html.UnescapeString(get(t, server, "/discover?type=movie&genre=878&year=2010&rating=7&sort=newest", true).Body.String())

	if len(details.discover) != 1 || details.discover[0] != "{MediaType:movie Genre:878 Year:2010 MinRating:7 Sort:newest} 1" {
		t.Errorf("TMDB was asked %v", details.discover)
	}
	for _, want := range []string{
		`href="/movie/27205"`, `action="/discover"`,
		`value="movie" checked`, `value="878" selected`, `value="2010" selected`, `value="7" selected`, `value="newest" selected`,
		"@get('/api/discover/page?genre=878&page=2&rating=7&sort=newest&type=movie&year=2010')",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the discover page lacks %q", want)
		}
	}
}

func TestDiscoverIgnoresFiltersItDoesNotKnow(t *testing.T) {
	details := discoverDetails()
	server := newMediaServer(t, details, &fakeJacRed{}, "")
	get(t, server, "/discover?type=bogus&genre=10765&year=1700&rating=99&sort=random", true)
	if len(details.discover) != 1 || details.discover[0] != "{MediaType:movie Genre:0 Year:0 MinRating:0 Sort:popular} 1" {
		t.Errorf("TMDB was asked %v, want movies by popularity, unfiltered", details.discover)
	}
}

func TestDiscoverLoadsTheNextPageAsYouScroll(t *testing.T) {
	details := discoverDetails()
	server := newMediaServer(t, details, &fakeJacRed{}, "")
	body := get(t, server, "/api/discover/page?type=tv&genre=10765&page=3", true).Body.String()
	if len(details.discover) != 1 || details.discover[0] != "{MediaType:tv Genre:10765 Year:0 MinRating:0 Sort:popular} 3" {
		t.Errorf("TMDB was asked %v", details.discover)
	}
	if !strings.Contains(body, "browse-grid") || !strings.Contains(body, `href="/movie/27205"`) || !strings.Contains(body, "That is the whole list.") {
		t.Errorf("page 3 of 3 does not end the grid:\n%s", body)
	}
	if rec := get(t, server, "/api/discover/page?page=2", false); rec.Code != http.StatusUnauthorized {
		t.Errorf("signed out = %d, want 401", rec.Code)
	}
}

func TestDiscoverIsForSignedInUsersAndLinkedFromHome(t *testing.T) {
	cfg := validTestConfig(t)
	cfg.Details = discoverDetails()
	server := newTestServerWithConfig(t, cfg)
	if rec := get(t, server, "/discover", false); rec.Code != http.StatusSeeOther {
		t.Errorf("signed out = %d, want a redirect to sign in", rec.Code)
	}
	if !regexp.MustCompile(`href="/discover"`).MatchString(get(t, server, "/movies", true).Body.String()) {
		t.Error("home does not lead to Discover")
	}
}
