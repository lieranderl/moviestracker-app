package views_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/jacred"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// Releases can be picked, up to four, and compared side by side; what a
// tracker did not report shows as unknown, and the table says playback is
// not checked.
func TestReleasesCanBeComparedSideBySide(t *testing.T) {
	results := []jacred.Result{
		{Tracker: "knaben", Title: "The Odyssey 2026 2160p WEB-DL HEVC 10bit Atmos", Quality: 2160, HDR: true, SizeName: "24.1 GB", Seeders: 48, Peers: 3, Voices: []string{"Dub"}},
		{Tracker: "bitru", Title: "The Odyssey 2026 1080p", Quality: 1080, Seeders: 153},
	}
	out := html.UnescapeString(render(t, views.TorrentResults(results, "seeders", "", false)))
	gen := regexp.MustCompile(`data-bind="_cmp\.(g[0-9a-f]+)\.r0"`).FindStringSubmatch(out)
	if gen == nil {
		t.Fatal("releases have no comparison pick")
	}
	for _, want := range []string{
		`data-bind="_cmp.` + gen[1] + `.r1"`, // a pick per release
		"Compare releases",
		`data-show="$_cmp.` + gen[1] + `.r1"`, // a column per release, shown once picked
		"Atmos", "HEVC", "24.1 GB",
		"Playback is not checked",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the comparison lacks %q", want)
		}
	}
	// The 1080p release names no codec, audio or size: unknown, not "no".
	column := regexp.MustCompile(`data-show="\$_cmp\.`+gen[1]+`\.r1"[^>]*>([^<]*)<`).FindAllStringSubmatch(out, -1)
	unknown := 0
	for _, c := range column {
		if strings.TrimSpace(c[1]) == "Unknown" {
			unknown++
		}
	}
	if unknown < 3 {
		t.Errorf("the 1080p release shows %d unknown values, want its codec, audio and size unknown: %v", unknown, column)
	}
	if !regexp.MustCompile(`data-attr:disabled="[^"]*>= 4`).MatchString(out) {
		t.Error("picking stops at four releases")
	}
}

// Picks belong to one set of results: a newer search's rows start unpicked,
// so picks made among older results neither count nor show.
func TestANewSearchStartsItsOwnComparison(t *testing.T) {
	gen := regexp.MustCompile(`data-bind="_cmp\.(g[0-9a-f]+)\.r0"`)
	first := gen.FindStringSubmatch(render(t, views.TorrentResults(releases(), "seeders", "", false)))
	second := gen.FindStringSubmatch(render(t, views.TorrentResults(releases()[:2], "seeders", "", false)))
	if first == nil || second == nil || first[1] == second[1] {
		t.Errorf("two different searches share their picks: %v, %v", first, second)
	}
}
