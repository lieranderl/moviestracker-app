package views_test

import (
	"github.com/lieranderl/moviestracker-app/internal/views"
	"html"
	"strings"
	"testing"
)

func TestBothPlayersOfferCompletionOnlyAtTheirNativeEndingBoundary(t *testing.T) {
	for app, page := range map[string]string{"local": torrPage(t, true), "web": html.UnescapeString(render(t, views.WebTorrServerPage(testUser, nil, nil)))} {
		for _, want := range []string{`id="player-completion"`, `data-on:seeking=`, `data-on:seeked=`, `/api/player/completion?hash=`, `data-request-key=`} {
			if !strings.Contains(page, want) {
				t.Errorf("%s player lacks native completion boundary %q", app, want)
			}
		}
	}
}
