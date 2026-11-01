package views_test

import (
	"context"
	"html"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/views"
)

// On the web app a file TorrServer no longer has is explained in the
// player, which leads back to its title's sources.
func TestTheWebPlayerLeadsBackToSourcesForAMissingFile(t *testing.T) {
	ctx := views.WithPlayRequest(context.Background(), views.PlayRequest{Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 2, Title: "Dune", Kind: "movie", TMDBID: 438631})
	out := html.UnescapeString(renderIn(t, ctx, views.WebTorrServerPage(testUser, nil, nil)))
	for _, want := range []string{
		`data-show="$_tsPlayerProblem === 'missing'"`,
		"This file is no longer on your TorrServer.",
		`data-attr:href="$_resumeHref"`,
		`$_resumeHref = '/movie/438631#sources'`,
		`data-show="($_tsPlayerProblem && $_tsPlayerProblem !== 'missing') || $_tsProbeProblem"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("web player lacks %q", want)
		}
	}
}
