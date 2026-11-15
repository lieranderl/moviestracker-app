package views_test

import (
	"context"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// The chooser is a chip at the head of the category bar, not a card of its
// own between the rows; it stays when TMDB sent no billboard.
func TestTheChooserIsTheFirstChipOfTheCategoryBar(t *testing.T) {
	for _, hero := range [][]tmdb.MediaItem{nil, {{ID: 1, Title: "Dune", MediaType: "movie"}}} {
		out := html.UnescapeString(render(t, views.Movies(testUser, hero, nil, nil, true, nil)))
		start := strings.Index(out, `id="category-bar"`)
		if start < 0 {
			t.Fatal("home has no category bar")
		}
		bar, _, _ := strings.Cut(out[start:], "</nav>")
		chooser, discover := strings.Index(bar, `href="/discover?pick=1"`), strings.Index(bar, `href="/discover"`)
		if chooser < 0 || discover < chooser || !strings.Contains(bar, "Help me choose") {
			t.Errorf("the category bar does not open with the chooser:\n%s", bar)
		}
		if chip := regexp.MustCompile(`<a href="/discover\?pick=1"[^>]*>`).FindString(bar); strings.Contains(chip, "btn-primary") {
			t.Errorf("the chooser looks like the chip of the row in view: %s", chip)
		}
		if strings.Contains(out, "What shall we watch tonight?") {
			t.Error("home still has a chooser card between the rows")
		}
	}
	if out := render(t, views.Movies(testUser, nil, nil, nil, false, nil)); strings.Contains(out, "/discover?pick=1") {
		t.Error("without TMDB details there is nothing to choose from, yet the chooser shows")
	}
}

// The billboard's slide picker is a row of dots over the billboard: each
// names its slide for assistive tech, none spells it out on screen.
func TestTheBillboardPicksSlidesWithLabelledDots(t *testing.T) {
	hero := []tmdb.MediaItem{{ID: 1, Title: "Dune", MediaType: "movie"}, {ID: 2, Title: "Arrival", MediaType: "movie"}}
	out := html.UnescapeString(render(t, views.HeroBillboard(hero)))

	start := strings.Index(out, `aria-label="Choose slide"`)
	if start < 0 {
		t.Fatal("the billboard has no slide picker")
	}
	picker, _, _ := strings.Cut(out[start:], "</div>")
	dots := regexp.MustCompile(`<button[^>]*>`).FindAllString(picker, -1)
	if len(dots) != 2 {
		t.Fatalf("picker has %d buttons, want 2:\n%s", len(dots), picker)
	}
	for i, name := range []string{"Slide 1 of 2: Dune", "Slide 2 of 2: Arrival"} {
		if !strings.Contains(dots[i], `aria-label="`+name+`"`) {
			t.Errorf("dot %d is not labelled %q: %s", i+1, name, dots[i])
		}
	}
	if strings.Contains(picker, "-white") {
		t.Errorf("the picker colours itself outside the theme's tokens:\n%s", picker)
	}
	if strings.Contains(picker, ">Dune<") || strings.Contains(picker, ">Arrival<") {
		t.Errorf("the picker spells out slide titles:\n%s", picker)
	}
}

// Home's rows share one header: title and icon alike, the icon in one tone
// for the user's own rows and another for the catalog, and links out alike.
func TestHomeRowsShareOneHeader(t *testing.T) {
	watching := []store.Progress{{Hash: "abc", Index: 1, Title: "Severance", Position: 60e9, Duration: 3000e9}}
	home := html.UnescapeString(render(t, views.Movies(testUser, nil, nil, nil, true, watching)))
	recs := html.UnescapeString(render(t, views.RecommendationRows([]views.RecommendationRow{{Title: "Because you watched Dune", Items: []tmdb.MediaItem{{ID: 2, Title: "Arrival", MediaType: "movie"}}}}, views.RecommendationFeedback{})))
	torrents := html.UnescapeString(render(t, views.RecentTorrents([]torrserver.Torrent{{Hash: "a", Title: "Dune", Timestamp: 1}})))

	header := regexp.MustCompile(`<header[^>]*>`)
	for name, out := range map[string]string{"up next": home[strings.Index(home, `id="up-next"`):], "movies": home[strings.Index(home, `id="home-movies"`):], "recommendations": recs, "torrents": torrents} {
		if got, want := header.FindString(out), header.FindString(home[strings.Index(home, `id="home-movies"`):]); got != want {
			t.Errorf("the %s row's header is %q, the movies one %q", name, got, want)
		}
	}
	heading := regexp.MustCompile(`<h2[^>]*class="([^"]*)"[^>]*>\s*<svg[^>]*class="([^"]*)"`)
	headings := map[string][]string{}
	for name, out := range map[string]string{"up next": home[strings.Index(home, `id="up-next"`):], "movies": home[strings.Index(home, `id="home-movies"`):], "recommendations": recs, "torrents": torrents} {
		m := heading.FindStringSubmatch(out)
		if m == nil {
			t.Fatalf("the %s row has no heading with an icon", name)
		}
		headings[name] = m[1:]
	}
	for name, h := range headings {
		if h[0] != headings["movies"][0] {
			t.Errorf("the %s heading is styled %q, the movies one %q", name, h[0], headings["movies"][0])
		}
		tone := "text-secondary"
		if name == "movies" {
			tone = "text-primary"
		}
		if !strings.Contains(h[1], tone) {
			t.Errorf("the %s icon is %q, want %s", name, h[1], tone)
		}
	}

	link := regexp.MustCompile(`<a href="([^"]*)" class="([^"]*)"[^>]*>\s*(?:<span[^>]*>[^<]*</span>)?\s*<svg[^>]*><path d="M5 12h14"/>`)
	classes := map[string]string{}
	for _, out := range []string{home, torrents} {
		for _, m := range link.FindAllStringSubmatch(out, -1) {
			classes[m[1]] = m[2]
		}
	}
	for _, href := range []string{"/calendar", "/torrserver", "/browse/trending-movies"} {
		if classes[href] == "" {
			t.Errorf("the link to %s is not a row link with an arrow", href)
		} else if classes[href] != classes["/calendar"] {
			t.Errorf("the link to %s is styled %q, the calendar one %q", href, classes[href], classes["/calendar"])
		}
	}
}

// Feedback on a recommended title sits with the poster's quick actions, so
// a recommendation card is as tall as any other.
func TestRecommendationFeedbackSitsOnThePoster(t *testing.T) {
	item := tmdb.MediaItem{ID: 2, Title: "Arrival", MediaType: "movie"}
	ctx := views.WithSignedIn(context.Background())
	out := html.UnescapeString(renderIn(t, ctx, views.RecommendationRows([]views.RecommendationRow{{Title: "Because you watched Dune", Items: []tmdb.MediaItem{item}}}, views.RecommendationFeedback{Allow: true})))

	actions := regexp.MustCompile(`(?s)<div class="join absolute[^"]*">.*?</div>`).FindString(out)
	for _, want := range []string{"Add Arrival to Want to watch", "Recommend more titles like Arrival", "Not interested in Arrival",
		"feedback=moreLikeThis&on=1", "feedback=notInterested&on=1"} {
		if !strings.Contains(actions, want) {
			t.Errorf("the poster's actions lack %q:\n%s", want, actions)
		}
	}
	if strings.Contains(out, "btn-xs") || strings.Contains(out, ">More like this<") {
		t.Error("feedback buttons are still under the card")
	}
}

// On a phone a row's link out keeps to its title's line: it shows only its
// arrow (its words stay for assistive tech), and a rail's tabs take the
// line below.
func TestOnPhonesARowsLinkKeepsToItsTitlesLine(t *testing.T) {
	watching := []store.Progress{{Hash: "abc", Index: 1, Title: "Severance", Position: 60e9, Duration: 3000e9}}
	home := html.UnescapeString(render(t, views.Movies(testUser, nil, nil, nil, true, watching)))

	for _, href := range []string{"/calendar", "/browse/trending-movies"} {
		link := regexp.MustCompile(`(?s)<a href="` + regexp.QuoteMeta(href) + `" class="btn btn-ghost[^>]*>.*?</a>`).FindString(home)
		if !strings.Contains(link, "max-sm:btn-square") || !regexp.MustCompile(`<span class="max-sm:sr-only">[^<]+</span>`).MatchString(link) {
			t.Errorf("the link to %s spells itself out on phones: %s", href, link)
		}
	}
	tabs := regexp.MustCompile(`<div role="tablist"[^>]*>`).FindString(home[strings.Index(home, `id="home-movies"`):])
	if !strings.Contains(tabs, "max-sm:order-last") || !strings.Contains(tabs, "max-sm:basis-full") {
		t.Errorf("a rail's tabs share its title's line on phones: %s", tabs)
	}
}

// On a desktop the billboard leaves room for the first row's heading: it is
// at most 640px tall, and shorter on a short window, but never shorter than
// the 520px its slide needs (the slides cannot grow it).
func TestTheBillboardLeavesRoomForTheFirstRow(t *testing.T) {
	out := render(t, views.HeroBillboard([]tmdb.MediaItem{{ID: 1, Title: "Dune", MediaType: "movie"}}))
	tag := regexp.MustCompile(`<div id="hero-carousel"[^>]*>`).FindString(out)
	if !strings.Contains(tag, "lg:min-h-[clamp(520px,calc(100svh-240px),640px)]") || strings.Contains(tag, "lg:min-h-[640px]") {
		t.Errorf("the billboard takes a fixed 640px on desktop: %s", tag)
	}
}
