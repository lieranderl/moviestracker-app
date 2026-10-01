package views_test

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/auth"
	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/jacred"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"

	"github.com/a-h/templ"
)

var testUser = &auth.User{Username: "alex", Name: "Alex", Role: "admin"}

func TestMoviePageShowsBudgetAndBoxOfficeFromCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id": 1, "title": "Example", "budget": 160000000, "revenue": 2923706026}`))
	}))
	t.Cleanup(server.Close)
	client := tmdb.NewClient("test-key", tmdb.WithBaseURL(server.URL), tmdb.WithHTTPClient(server.Client()))
	movie, err := client.Movie(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, cloud := range []bool{false, true} {
		ctx := views.WithSite(context.Background(), views.Site{Cloud: cloud})
		var out strings.Builder
		if err := views.MoviePage(testUser, movie).Render(ctx, &out); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Budget", "$160,000,000", "Box office", "$2,923,706,026"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("cloud=%t: movie details missing %q", cloud, want)
			}
		}
	}
}

func TestMoviePageOmitsUnavailableFinances(t *testing.T) {
	for _, tc := range []struct {
		name            string
		budget, revenue int64
		wantBudget      bool
		wantBoxOffice   bool
	}{
		{name: "unknown"},
		{name: "budget only", budget: 160000000, wantBudget: true},
		{name: "box office only", revenue: 2923706026, wantBoxOffice: true},
		{name: "invalid amounts", budget: -1, revenue: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			movie := &tmdb.MovieDetails{MediaItem: tmdb.MediaItem{ID: 1, Title: "Example", MediaType: "movie"}, Budget: tc.budget, Revenue: tc.revenue}
			for _, cloud := range []bool{false, true} {
				for _, lang := range []i18n.Lang{i18n.English, i18n.Russian} {
					ctx := i18n.WithLang(views.WithSite(context.Background(), views.Site{Cloud: cloud}), lang)
					var out strings.Builder
					if err := views.MoviePage(testUser, movie).Render(ctx, &out); err != nil {
						t.Fatal(err)
					}
					for label, want := range map[string]bool{"Budget": tc.wantBudget, "Box office": tc.wantBoxOffice} {
						if got := strings.Contains(out.String(), ">"+i18n.T(ctx, label)+"</dt>"); got != want {
							t.Errorf("cloud=%t lang=%s: %s shown=%t, want %t", cloud, lang, label, got, want)
						}
					}
				}
			}
		})
	}
}

const hostileName = `Evil'); alert(1); ('"<b>`

func render(t *testing.T, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	if err := c.Render(context.Background(), &sb); err != nil {
		t.Fatalf("render failed: %v", err)
	}
	return sb.String()
}

func allPages() map[string]templ.Component {
	media := []tmdb.MediaItem{
		{ID: 1, Title: "Cosmic Horizons", MediaType: "movie", TrailerKey: "abc123", ImdbID: "tt1234567", VoteAverage: 8.1, VoteCount: 1500},
		{ID: 2, Title: "Cyberpunk Legacy", MediaType: "tv", VoteAverage: 7.2},
	}
	torrents := []torrserver.Torrent{
		{Hash: "hash1", Title: hostileName, FileStats: []torrserver.FileStat{{ID: 1, Path: "movie.mkv", Length: 1 << 30}, {ID: 2, Path: "notes.txt", Length: 10}}},
		{Hash: "hash2", Title: "No Files Yet"},
	}
	return map[string]templ.Component{
		"consent": views.Login(false, ""),
		"login":   views.Login(true, "Invalid username or password"),
		"movies":  views.Movies(testUser, media, media, media, true),
		"movie": views.MoviePage(testUser, &tmdb.MovieDetails{
			MediaItem: tmdb.MediaItem{ID: 27205, Title: hostileName, MediaType: "movie", ReleaseDate: "2010-07-15", ImdbID: "tt1375666", TrailerKey: "YoHD9XEInc0"},
			Homepage:  "https://example.com",
			Links:     tmdb.ExternalIDs{IMDb: "tt1375666", Wikidata: "Q1", Facebook: "f", Instagram: "i", Twitter: "t", TikTok: "k", YouTube: "@y"},
			Cast:      []tmdb.CastMember{{ID: 1, Name: hostileName}},
			Directors: []tmdb.CrewMember{{ID: 2, Name: "Christopher Nolan"}},
			Videos:    []tmdb.Video{{Key: "YoHD9XEInc0", Name: hostileName}},
		}),
		"person": views.PersonPage(testUser, &tmdb.Person{ID: 6193, Name: hostileName, Biography: strings.Repeat("A long biography. ", 60),
			Credits: []tmdb.Credit{{MediaItem: tmdb.MediaItem{ID: 1, Title: hostileName, MediaType: "tv"}, Role: hostileName}}}),
		"torrent-results": views.TorrentResults(hostileReleases, "seeders", views.SourcesSearch("movie", 1)),
		"discover-rail":   views.DiscoverRailLoaded(views.DiscoverRails[0], media),
		"search": views.SearchPage(testUser, views.SearchView{Query: hostileName, Results: &tmdb.SearchResults{
			Movies: media, People: []tmdb.PersonSummary{{ID: 1, Name: hostileName, KnownFor: []string{hostileName}}},
		}}),
		"discover": views.SearchPage(testUser, views.SearchView{TrendingMovies: media}),
		"tv": views.TVPage(testUser, &tmdb.TVDetails{
			MediaItem: tmdb.MediaItem{ID: 1396, Title: hostileName, MediaType: "tv", ReleaseDate: "2008-01-20"},
			Seasons:   []tmdb.SeasonSummary{{Number: 1}, {Number: 0}},
		}, &tmdb.Season{Number: 1, Episodes: []tmdb.Episode{{SeasonNumber: 1, Number: 1, Name: hostileName}}}, 1),
		"torrserver": views.TorrServer(testUser, "http://localhost:8090", "http://192.168.1.20:8095", testLinks, torrserver.EchoInfo{Version: "1.0"}, torrents, views.GStreamerSetup{}),
	}
}

func TestEveryIconRendersArtwork(t *testing.T) {
	emptySVG := regexp.MustCompile(`<svg[^>]*class="lucide[^"]*"[^>]*>\s*</svg>`)
	for name, page := range allPages() {
		out := render(t, page)
		if m := emptySVG.FindString(out); m != "" {
			t.Errorf("%s: icon rendered without artwork (unknown icon name?): %s", name, m)
		}
		if strings.Contains(out, "data-lucide") {
			t.Errorf("%s: client-side Lucide placeholder found; icons must be server-rendered", name)
		}
	}
}

func TestEveryBrandLogoRendersArtwork(t *testing.T) {
	out := render(t, allPages()["movie"])
	if n := strings.Count(out, `fill="currentColor"`); n < 7 {
		t.Fatalf("brand logos rendered = %d, want 7", n)
	}
	if strings.Contains(out, `<path d=""`) {
		t.Error("brand logo rendered without artwork (slug missing from icons_gen.go?)")
	}
}

// data-show only toggles the inline display style, so it can never reveal an
// element hidden by the "hidden" class; such elements must use data-class.
func TestDataShowNeverFightsHiddenClass(t *testing.T) {
	tag := regexp.MustCompile(`<[a-z]+[^>]*\bdata-show[^>]*>`)
	hiddenClass := regexp.MustCompile(`class="[^"]*\bhidden\b`)
	for name, page := range allPages() {
		for _, m := range tag.FindAllString(render(t, page), -1) {
			if hiddenClass.MatchString(m) {
				t.Errorf("%s: element can never be shown: %s", name, m)
			}
		}
	}
}

func TestPagesAvoidInlineStylesAndScripts(t *testing.T) {
	for name, page := range allPages() {
		out := render(t, page)
		for _, forbidden := range []string{" style=", "<style", "<script>"} {
			if strings.Contains(out, forbidden) {
				t.Errorf("%s: contains CSP-incompatible markup %q", name, forbidden)
			}
		}
	}
}

var hostileReleases = []jacred.Result{{
	Tracker: "rutor", Title: hostileName, SourceURL: "https://rutor.info/1",
	Magnet: "magnet:?xt=urn:btih:abc&dn=" + hostileName, Quality: 1080, Voices: []string{hostileName},
}}

func TestUpstreamTextNeverEntersDatastarExpressions(t *testing.T) {
	expr := regexp.MustCompile(`data-(?:on|show|effect|init|text|class|attr|signals)[\w:.\-]*="([^"]*)"`)
	for _, name := range []string{"movie", "tv", "person", "search", "torrent-results"} {
		out := render(t, allPages()[name])
		for _, m := range expr.FindAllStringSubmatch(out, -1) {
			if strings.Contains(html.UnescapeString(m[1]), "alert(1)") {
				t.Fatalf("%s: upstream text leaked into a Datastar expression: %s", name, m[0])
			}
		}
	}
}

func TestTorrentNamesNeverEnterDatastarExpressions(t *testing.T) {
	out := render(t, allPages()["torrserver"])
	expr := regexp.MustCompile(`data-(?:on|show|effect|init|text|class|attr)[\w:.\-]*="([^"]*)"`)
	for _, m := range expr.FindAllStringSubmatch(out, -1) {
		if strings.Contains(html.UnescapeString(m[1]), "alert(1)") {
			t.Fatalf("torrent name leaked into a Datastar expression: %s", m[0])
		}
	}
	if !strings.Contains(out, `data-title="`+html.EscapeString(hostileName)+`"`) {
		t.Errorf("expected torrent name to be passed as an escaped data attribute")
	}
}

func TestTorrServerModalsAreNativeDialogs(t *testing.T) {
	out := render(t, allPages()["torrserver"])
	for _, id := range []string{"torr-player-modal", "torr-add-modal", "torr-probe-modal", "torr-confirm-modal"} {
		if !strings.Contains(out, `<dialog id="`+id+`"`) {
			t.Errorf("modal %q should be a native <dialog>", id)
		}
	}
	if strings.Contains(out, "torr-files-modal") {
		t.Errorf("unused files modal should not be rendered")
	}
}

func TestTorrentSearchLivesOutsideStreamedFragment(t *testing.T) {
	fragment := render(t, views.TorrServerLiveFragment(nil, torrserver.EchoInfo{}, "http://a", testLinks, false))
	if strings.Contains(fragment, "data-bind:filter-query") {
		t.Fatalf("search input must not be part of the streamed fragment")
	}
	for _, id := range []string{"torr-status", "torr-active-server", "torr-count", "torr-list-container"} {
		if !strings.Contains(fragment, `id="`+id+`"`) {
			t.Errorf("live fragment missing region %q", id)
		}
	}
}

func TestLoginErrorAlertIsSignalDriven(t *testing.T) {
	out := html.UnescapeString(render(t, views.Login(true, "Invalid username or password")))
	if !strings.Contains(out, `data-class:hidden="!$errorMessage"`) || !strings.Contains(out, `data-text="$errorMessage"`) {
		t.Errorf("login error alert should be bound to $errorMessage")
	}
}

func TestLogoutSubmitsOnce(t *testing.T) {
	out := render(t, views.Navbar(testUser))
	if strings.Contains(out, `data-on:click="@post('/api/logout')"`) {
		t.Errorf("logout must not combine a click @post with a native form submit")
	}
	if !strings.Contains(out, `data-on:submit="@post('/api/logout')"`) {
		t.Errorf("logout form should submit via Datastar on submit")
	}
}

func TestHeroLoadsEveryIMDbRatingWithOneRequest(t *testing.T) {
	hero := []tmdb.MediaItem{
		{ID: 1, Title: "A", ImdbID: "tt0000001", BackdropPath: "/a.jpg"},
		{ID: 2, Title: "B", BackdropPath: "/b.jpg"},
		{ID: 3, Title: "C", ImdbID: "tt0000003", BackdropPath: "/c.jpg"},
	}
	out := html.UnescapeString(render(t, views.HeroBillboard(hero)))
	if n := strings.Count(out, "/api/movies/imdb-rating"); n != 1 {
		t.Fatalf("IMDb rating requests = %d, want 1", n)
	}
	if !strings.Contains(out, "id=tt0000001&target=hero-imdb-rating-0&id=tt0000003&target=hero-imdb-rating-2") {
		t.Errorf("request does not pair each id with its slide's badge:\n%s", out)
	}
}
