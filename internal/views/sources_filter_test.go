package views_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/jacred"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

func releases() []jacred.Result {
	return []jacred.Result{
		{Tracker: "knaben", Title: "The Odyssey 2026 1080p", Quality: 1080, Seeders: 145},
		{Tracker: "knaben", Title: "The Odyssey 2026 2160p", Quality: 2160, Seeders: 48},
		{Tracker: "bitru", Title: "Одиссея / The Odyssey (2026)", Seeders: 153},
		{Tracker: `rutracker"><img src=x onerror=alert(1)>`, Title: "hostile tracker"},
	}
}

func TestReleasesCanBeFilteredByOneOrMoreTrackers(t *testing.T) {
	out := html.UnescapeString(render(t, views.TorrentResults(releases(), "seeders", "")))
	// A toggle per tracker, busiest first, with its count; All clears the choice.
	knaben, bitru := strings.Index(out, `value="knaben"`), strings.Index(out, `value="bitru"`)
	if knaben < 0 || bitru < 0 || knaben > bitru {
		t.Errorf("tracker toggles missing or not busiest first (knaben at %d, bitru at %d)", knaben, bitru)
	}
	for _, want := range []string{
		`aria-label="Filter by tracker"`, `data-bind="trackers"`, "Knaben", "BitRu",
		`data-on:click="$trackers = $trackers.map(() => '')"`, `data-class="{ 'btn-primary': !$trackers.some(Boolean) }"`,
		`data-tracker="knaben"`, "$trackers.includes(el.dataset.tracker)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("sources lack %q", want)
		}
	}
	if !regexp.MustCompile(`value="knaben"[^>]*>[\s\S]{0,200}?2<`).MatchString(out) {
		t.Error("the Knaben toggle does not show its 2 releases")
	}
}

func TestTrackerNamesNeverEnterDatastarExpressions(t *testing.T) {
	out := render(t, views.TorrentResults(releases(), "seeders", ""))
	expr := regexp.MustCompile(`data-(?:on|show|effect|init|text|class|attr|bind)[\w:.\-]*="([^"]*)"`)
	for _, m := range expr.FindAllStringSubmatch(out, -1) {
		if strings.Contains(html.UnescapeString(m[1]), "alert(1)") {
			t.Fatalf("a tracker name leaked into a Datastar expression: %s", m[0])
		}
	}
}

// The search carries the qualities and HDR picked before it.
func TestTheSourcesSearchSendsThePickedQualitiesAndHDR(t *testing.T) {
	search := views.SourcesSearch("movie", 27205)
	for _, want := range []string{"'&quality=' + $qual.filter(Boolean).join(',')", "($hdr ? '&hdr=1' : '')"} {
		if !strings.Contains(search, want) {
			t.Errorf("search %q lacks %q", search, want)
		}
	}
}

// Results filter by the voices (dubbing) they carry, any number of them;
// quality is picked before searching, so there are no quality tabs.
func TestReleasesCanBeFilteredByVoice(t *testing.T) {
	rs := []jacred.Result{
		{Tracker: "rutracker", Title: "A", Voices: []string{"Дубляж", "LostFilm"}},
		{Tracker: "kinozal", Title: "B", Voices: []string{"Дубляж"}},
		{Tracker: "rutor", Title: "C"},
	}
	out := html.UnescapeString(render(t, views.TorrentResults(rs, "seeders", "")))
	dub, lost := strings.Index(out, `value="Дубляж"`), strings.Index(out, `value="LostFilm"`)
	if dub < 0 || lost < 0 || dub > lost {
		t.Errorf("voice toggles missing or not commonest first (%d, %d)", dub, lost)
	}
	for _, want := range []string{
		`aria-label="Filter by voice"`, `data-bind="voices"`, `data-voices="Дубляж|LostFilm"`,
		"el.dataset.voices.split('|').some(v => v && $voices.includes(v))",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("results lack %q", want)
		}
	}
	if strings.Contains(out, `aria-label="Filter by quality"`) || strings.Contains(out, "$q ===") {
		t.Error("the quality tabs should be gone: quality is picked before searching")
	}
}
