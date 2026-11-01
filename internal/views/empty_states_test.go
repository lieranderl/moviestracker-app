package views_test

import (
	"context"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/lieranderl/moviestracker-app/internal/auth"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

func renderIn(t *testing.T, ctx context.Context, c templ.Component) string {
	t.Helper()
	var sb strings.Builder
	if err := c.Render(ctx, &sb); err != nil {
		t.Fatalf("render failed: %v", err)
	}
	return html.UnescapeString(sb.String())
}

// emptyState finds an empty state titled title and returns it.
func emptyState(t *testing.T, out, title string) string {
	t.Helper()
	card := regexp.MustCompile(`(?s)<div[^>]*data-empty-state[^>]*>.*?` + regexp.QuoteMeta(title) + `.*?</div>\s*</div>`).FindString(out)
	if card == "" {
		t.Fatalf("no empty state titled %q", title)
	}
	return card
}

func TestNoFavouritesYetLeadsToSomethingToKeep(t *testing.T) {
	ctx := views.WithSite(context.Background(), views.Site{Cloud: true})
	out := renderIn(t, ctx, views.FavoritesPage(&auth.User{Name: "Ann"}, nil, "", false))
	card := emptyState(t, out, "No favourites yet")
	if !regexp.MustCompile(`<a href="/" class="btn btn-primary[^"]*"[^>]*>[\s\S]*?Browse trending`).MatchString(card) {
		t.Error("the empty favourites do not lead to trending titles")
	}
}

func TestNoTorrServerYetOffersToAddOne(t *testing.T) {
	out := renderIn(t, context.Background(), views.WebTorrServers(nil))
	card := emptyState(t, out, "No TorrServer yet")
	if !strings.Contains(card, `data-on:click="$tsAddOpen = true"`) || !strings.Contains(card, "Add TorrServer") {
		t.Error("the empty TorrServer list does not offer to add one")
	}
	if !strings.Contains(card, `role="status"`) {
		t.Error("removing the last TorrServer is not announced: the empty list is no live status")
	}
}

func TestANewcomerWithoutATorrServerLearnsWhatItIsAndHowToInstallIt(t *testing.T) {
	out := renderIn(t, context.Background(), views.WebTorrServers(nil))
	for _, want := range []string{
		"What is TorrServer?",
		`href="https://github.com/YouROK/TorrServer"`,
		"Moviestracker never downloads, stores or relays any video",
		"TorrServer-gst-windows-amd64.exe",
		"Why GStreamer?",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the empty TorrServer list does not explain TorrServer: no %q", want)
		}
	}
	saved := renderIn(t, context.Background(), views.WebTorrServers([]store.TorrServer{{ID: "1", Name: "Home", URL: "http://localhost:8090"}}))
	if strings.Contains(saved, "What is TorrServer?") {
		t.Error("the explanation stays once the user has a TorrServer")
	}
}

func TestASearchWithNoMatchesLeadsBackToTrending(t *testing.T) {
	out := renderIn(t, context.Background(), views.SearchPage(&auth.User{Name: "Ann"}, views.SearchView{Query: "zzzz", Results: &tmdb.SearchResults{}}))
	card := emptyState(t, out, "No matches for “zzzz”")
	if !strings.Contains(card, "Try another title or name.") || !strings.Contains(card, "Browse trending") {
		t.Error("an empty search does not suggest what to do instead")
	}
}

func TestOpeningTheAddTorrServerFormScrollsToIt(t *testing.T) {
	out := renderIn(t, context.Background(), views.WebTorrServerPage(&auth.User{Name: "Ann"}, nil, nil))
	form := regexp.MustCompile(`<form[^>]*data-show="\$tsAddOpen"[^>]*>`).FindString(out)
	if form == "" {
		t.Fatal("no add TorrServer form")
	}
	if !strings.Contains(form, `data-effect="$tsAddOpen && el.scrollIntoView({behavior: 'smooth', block: 'center'})"`) {
		t.Errorf("opening the add TorrServer form does not scroll to it: %s", form)
	}
}

// The web app says a TorrServer has no GStreamer only when it answered so;
// a failed check is reported as such.
func TestTheWebTorrServerPanelTellsAFailedGStreamerCheckFromNoGStreamer(t *testing.T) {
	out := html.UnescapeString(renderIn(t, context.Background(), views.WebTorrServerPage(testUser, nil, nil)))
	for _, want := range []string{
		`data-show="$tsStatus.hlsKnown && !$tsStatus.hls"`,
		`data-show="!$tsStatus.hlsKnown"`, "Couldn't check whether this TorrServer can convert to HLS",
		"$gstKnown = $tsStatus.hlsKnown",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("web TorrServer page lacks %q", want)
		}
	}
}
