package views_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

var twoServers = []store.TorrServer{
	{ID: "a", Name: "Home", URL: "http://localhost:8090"},
	{ID: "b", Name: "TV box", URL: "https://tv.example:8091"},
}

// tagOf is the opening tag of the element with id in out.
func tagOf(t *testing.T, out, id string) string {
	t.Helper()
	tag := regexp.MustCompile(`<[a-z]+[^>]*\sid="` + id + `"[^>]*>`).FindString(out)
	if tag == "" {
		t.Fatalf("page has no #%s", id)
	}
	return tag
}

// Once a TorrServer answers, the page opens on its torrents: a one-line bar
// names the server and its connection, and the servers, their connection
// details and settings fold behind it.
func TestAConnectedTorrServerPageOpensOnItsTorrents(t *testing.T) {
	out := html.UnescapeString(render(t, views.WebTorrServerPage(testUser, twoServers, nil)))

	bar := tagOf(t, out, "ts-bar")
	if !strings.Contains(bar, `data-show="$tsStatus.ok || $tsStatus.checking"`) {
		t.Errorf("the bar does not show while a TorrServer is checked or connected: %s", bar)
	}
	barBody := out[strings.Index(out, `id="ts-bar"`):]
	barBody, _, _ = strings.Cut(barBody, `id="ts-manage"`)
	for _, want := range []string{
		`data-show="$tsSelected === 'http://localhost:8090'"`, "Home",
		`data-show="$tsSelected === 'https://tv.example:8091'"`, "TV box",
		"Checking…", "Connected",
		`aria-controls="ts-manage"`, `data-attr:aria-expanded="($_tsManageOpen || $tsAddOpen) ? 'true' : 'false'"`,
		// Closing the servers closes the add form too, which also unfolds them.
		`const open = $_tsManageOpen || $tsAddOpen; $_tsManageOpen = !open; if (open) { $tsAddOpen = false }`,
		"$_tsManageOpen = true; $_tsSettingsOpen = true; $_tsSettingsJump++",
	} {
		if !strings.Contains(barBody, want) {
			t.Errorf("the bar lacks %q", want)
		}
	}

	manage := tagOf(t, out, "ts-manage")
	if !strings.Contains(manage, `data-show="!($tsStatus.ok || $tsStatus.checking) || $_tsManageOpen || $tsAddOpen"`) {
		t.Errorf("the servers do not fold away once connected: %s", manage)
	}
	order := []string{`id="ts-bar"`, `id="ts-manage"`, `id="ts-servers"`, `id="ts-status"`, `id="ts-settings"`, `id="ts-torrents-box"`}
	at := -1
	for _, id := range order {
		i := strings.Index(out, id)
		if i < at {
			t.Errorf("page has %s out of order %q", id, order)
		}
		at = i
	}
	if !strings.Contains(out, "tsStatus: {checking: true,") {
		t.Error("with TorrServers the page does not start by checking, so it opens on the server list")
	}
}

// With no TorrServer there is nothing to check: the page opens as before,
// on the explanation and Add TorrServer.
func TestAPageWithoutTorrServersOpensOnAddingOne(t *testing.T) {
	out := html.UnescapeString(render(t, views.WebTorrServerPage(testUser, nil, nil)))
	if !strings.Contains(out, "tsStatus: {checking: false,") {
		t.Error("a page with no TorrServer starts checking one")
	}
}

// The header's two add buttons sit together at its end.
func TestTheTorrServerHeaderGroupsItsButtons(t *testing.T) {
	out := html.UnescapeString(render(t, views.WebTorrServerPage(testUser, twoServers, nil)))
	head := regexp.MustCompile(`(?s)<header class="flex[^"]*">.*?</header>`).FindString(out)
	group := regexp.MustCompile(`(?s)<div class="flex[^"]*">\s*<button[^>]*>.*?Add Torrents.*?Add TorrServer.*?</div>`).FindString(head)
	if group == "" {
		t.Errorf("the header's buttons are not grouped:\n%s", head)
	}
}

// A link to the connection details unfolds them, wherever they are folded.
func TestTheConnectionLinkUnfoldsTheServers(t *testing.T) {
	out := html.UnescapeString(render(t, views.WebTorrServerPage(testUser, twoServers, nil)))
	link := regexp.MustCompile(`<a href="#ts-status"[^>]*>`).FindString(out)
	if !strings.Contains(link, "$_tsManageOpen = true") {
		t.Errorf("the link to the connection details leaves them folded: %s", link)
	}
}

// A TorrServer's address is quoted as a JavaScript string wherever an
// expression names it, so an odd address cannot break out of it.
func TestTheBarQuotesTorrServerAddresses(t *testing.T) {
	odd := []store.TorrServer{{ID: "x", Name: "Odd", URL: "http://nas:8090/a'b"}}
	out := render(t, views.WebTorrServerPage(testUser, odd, nil))
	if strings.Contains(html.UnescapeString(out), "=== 'http://nas:8090/a'b'") {
		t.Error("the bar puts an address into an expression unquoted")
	}
}

// Opening the settings from the bar brings them into view, below the
// servers and the connection.
func TestOpeningSettingsScrollsToThem(t *testing.T) {
	out := html.UnescapeString(render(t, views.WebTorrServerPage(testUser, twoServers, nil)))
	settings := out[strings.Index(out, `id="ts-settings"`):]
	// Every press scrolls, also when the settings were open already.
	if !strings.Contains(settings, `data-effect="$_tsSettingsJump && requestAnimationFrame(() => el.closest('#ts-settings').scrollIntoView({behavior: 'smooth', block: 'start'}))"`) {
		t.Error("opening the settings leaves them out of view")
	}
	if !strings.Contains(out, "_tsSettingsJump: 0") {
		t.Error("the page does not count presses of Settings")
	}
}

// The bar and the connection card show the connection with the same badges.
func TestTheBarAndTheConnectionCardShareTheirBadges(t *testing.T) {
	out := html.UnescapeString(render(t, views.WebTorrServerPage(testUser, twoServers, nil)))
	connected := regexp.MustCompile(`<span class="badge badge-success badge-soft gap-1.5"[^>]*>`).FindAllString(out, -1)
	hls := regexp.MustCompile(`<span class="badge badge-info badge-soft gap-1.5[^"]*"[^>]*>`).FindAllString(out, -1)
	if len(connected) != 2 || len(hls) != 2 || connected[0] != connected[1] {
		t.Errorf("the bar and the card draw the connection apart: %q %q", connected, hls)
	}
}
