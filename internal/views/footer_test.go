package views_test

import (
	"html"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/views"
)

func TestTheFooterCreditsTheStreamingEngineAndShowsTheVersion(t *testing.T) {
	views.SetVersion("v0.1.30")
	defer views.SetVersion("")
	out := html.UnescapeString(render(t, views.Movies(testUser, nil, nil, nil, true)))
	footer := out[strings.Index(out, "<footer"):]
	for _, want := range []string{
		`href="https://github.com/YouROK/TorrServer"`, ">TorrServer",
		`href="https://gstreamer.freedesktop.org/"`, ">GStreamer",
		"Moviestracker v0.1.30",
	} {
		if !strings.Contains(footer, want) {
			t.Errorf("footer lacks %q", want)
		}
	}
	if login := render(t, views.Login(true, "")); strings.Contains(login, "v0.1.30") {
		t.Error("the sign-in page tells strangers which version runs")
	}
}

// AGPL-3.0 section 13: everyone who uses Moviestracker over the network, signed
// in or not, is offered its source code.
func TestEveryPageOffersTheSourceCodeUnderTheAGPL(t *testing.T) {
	pages := map[string]string{
		"catalog": render(t, views.Movies(testUser, nil, nil, nil, true)),
		"sign-in": render(t, views.Login(true, "")),
	}
	for name, out := range pages {
		footer := html.UnescapeString(out[strings.Index(out, "<footer"):])
		for _, want := range []string{`href="https://github.com/lieranderl/moviestracker-app"`, "Source code", "AGPL-3.0"} {
			if !strings.Contains(footer, want) {
				t.Errorf("the %s page's footer lacks %q", name, want)
			}
		}
	}
}
