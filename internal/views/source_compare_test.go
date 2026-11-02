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
	for _, want := range []string{
		`data-bind="_cmp.r0"`, `data-bind="_cmp.r1"`, // a pick per release
		"Compare releases",
		`data-show="$_cmp.r1"`, // a column per release, shown once picked
		"Atmos", "HEVC", "24.1 GB",
		"Playback is not checked",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the comparison lacks %q", want)
		}
	}
	// The 1080p release names no codec, audio or size: unknown, not "no".
	column := regexp.MustCompile(`data-show="\$_cmp\.r1"[^>]*>([^<]*)<`).FindAllStringSubmatch(out, -1)
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
