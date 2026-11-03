package views_test

import (
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/views"
)

func TestBothFileMenusExplainPlaybackReadinessWithoutClaimingCodecSupport(t *testing.T) {
	for app, page := range map[string]string{"local": torrPage(t, true), "web": html.UnescapeString(render(t, views.WebTorrServerPage(testUser, nil, nil)))} {
		menu := regexp.MustCompile(`(?s)<dialog id="torr-file-menu".*?</dialog>`).FindString(page)
		for _, want := range []string{"Playback readiness", "Connection", "HLS conversion", "Filename hint", "Direct playback", "Not checked", "A filename does not confirm browser codec support.", `role="status"`} {
			if !strings.Contains(menu, want) {
				t.Errorf("%s menu lacks %q", app, want)
			}
		}
		if app == "local" && !strings.Contains(menu, "/api/torrserver/readiness") {
			t.Error("local menu lacks connection-only Refresh")
		}
		if app == "web" && (!strings.Contains(menu, "tsCheck($_connectionCheck, $tsSelected, 'ts-readiness')") || !strings.Contains(menu, "hlsKnown")) {
			t.Error("web menu does not adapt existing browser capabilities")
		}
	}
}
