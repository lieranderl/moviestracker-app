package views_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// A mouse has no sideways scroll: every carousel has previous and next
// buttons that scroll it, shown where there is a mouse (touch screens swipe).
func TestCarouselsScrollWithPreviousAndNextButtons(t *testing.T) {
	out := render(t, views.DiscoverRailLoaded(views.DiscoverRails[0], []tmdb.MediaItem{{ID: 1, Title: "A"}, {ID: 2, Title: "B"}}))
	for label, dir := range map[string]string{"Scroll back": "-", "Scroll forward": ""} {
		b := regexp.MustCompile(`<button[^>]*aria-label="` + label + `"[^>]*>`).FindString(out)
		if b == "" {
			t.Fatalf("carousel lacks %q", label)
		}
		if !strings.Contains(b, "scrollBy({left: "+dir+"") || !strings.Contains(b, "pointer-fine:") {
			t.Errorf("%q should scroll the carousel, on mouse screens: %s", label, b)
		}
	}
}
