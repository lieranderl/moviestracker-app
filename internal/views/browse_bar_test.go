package views_test

import (
	"context"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/views"
)

// browsePage renders the browse page of slug on ctx's site.
func browsePage(t *testing.T, ctx context.Context, slug string) string {
	t.Helper()
	l, ok := views.FindBrowseList(ctx, slug)
	if !ok {
		t.Fatalf("no list %s", slug)
	}
	return html.UnescapeString(renderIn(t, ctx, views.BrowsePage(testUser, views.BrowseView{List: l})))
}

// barOf is the category bar of a page.
func barOf(t *testing.T, out string) string {
	t.Helper()
	start := strings.Index(out, `id="category-bar"`)
	if start < 0 {
		t.Fatal("page has no category bar")
	}
	bar, _, _ := strings.Cut(out[start:], "</nav>")
	return bar
}

// A browse page's bar names the groups home has, not every list: Discover,
// Movies, Series (and Releases on the web app), the current one lit.
func TestABrowsePagesBarGroupsItsListsLikeHome(t *testing.T) {
	cloud := views.WithSite(context.Background(), views.Site{Cloud: true})
	bar := barOf(t, browsePage(t, cloud, "now-playing"))

	links := regexp.MustCompile(`<a href="([^"]*)"`).FindAllStringSubmatch(bar, -1)
	var hrefs []string
	for _, l := range links {
		hrefs = append(hrefs, l[1])
	}
	want := []string{"/discover", "/browse/trending-movies", "/browse/trending-series", "/browse/latest-releases"}
	if strings.Join(hrefs, " ") != strings.Join(want, " ") {
		t.Errorf("bar links = %v, want %v", hrefs, want)
	}
	current := regexp.MustCompile(`<a [^>]*aria-current="page"[^>]*>`).FindAllString(bar, -1)
	if len(current) != 1 || !strings.Contains(current[0], `href="/browse/trending-movies"`) {
		t.Errorf("In theaters should light the Movies chip: %q", current)
	}
	if local := barOf(t, browsePage(t, context.Background(), "now-playing")); strings.Contains(local, "releases") {
		t.Error("the local app has no release feeds, yet its bar offers them")
	}
}

// Under its heading a browse page offers its group's other lists as tabs;
// Popular movies, which home leaves out, is one of the movies'.
func TestABrowsePageSwitchesBetweenItsGroupsLists(t *testing.T) {
	out := browsePage(t, context.Background(), "now-playing")
	tabs := regexp.MustCompile(`(?s)<nav role="tablist"[^>]*aria-label="Movies"[^>]*>.*?</nav>`).FindString(out)
	for _, want := range []string{
		`href="/browse/trending-movies"`, `href="/browse/now-playing"`, `href="/browse/top-rated-movies"`, `href="/browse/popular-movies"`,
		">Trending<", ">In theaters<", ">Top rated<", ">Popular<",
	} {
		if !strings.Contains(tabs, want) {
			t.Errorf("the movies tabs lack %q:\n%s", want, tabs)
		}
	}
	active := regexp.MustCompile(`<a [^>]*class="tab tab-active"[^>]*>`).FindAllString(tabs, -1)
	if len(active) != 1 || !strings.Contains(active[0], `href="/browse/now-playing"`) || !strings.Contains(active[0], `aria-selected="true"`) {
		t.Errorf("the page's own list is not the selected tab: %q", active)
	}
}
