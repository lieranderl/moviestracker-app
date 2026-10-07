package views_test

import (
	"context"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
	"github.com/lieranderl/moviestracker-app/internal/auth"
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
	out := renderIn(t, ctx, views.FavoritesPage(&auth.User{Name: "Ann"}, nil, false))
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
}

func TestASearchWithNoMatchesLeadsBackToTrending(t *testing.T) {
	out := renderIn(t, context.Background(), views.SearchPage(&auth.User{Name: "Ann"}, views.SearchView{Query: "zzzz", Results: &tmdb.SearchResults{}}))
	card := emptyState(t, out, "No matches for “zzzz”")
	if !strings.Contains(card, "Try another title or name.") || !strings.Contains(card, "Browse trending") {
		t.Error("an empty search does not suggest what to do instead")
	}
}
