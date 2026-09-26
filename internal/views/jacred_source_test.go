package views_test

import (
	"html"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/views"
)

// From 9 Oct 2026 jacred.su needs a free personal key: the JacRed card says
// so, where to get it and what it allows.
func TestTheJacRedCardExplainsHowToGetAKey(t *testing.T) {
	v := views.SourcesView{JacRedURL: "https://jacred.su"} // #nosec G101 -- an address, not a credential
	out := html.UnescapeString(render(t, views.JacRedSource(v, views.SourceStatus{})))
	for _, want := range []string{
		`href="https://jacred.su/account"`, "9 October 2026", "100 searches a day", "Мой ключ",
		`placeholder="Your jacred.su key, or a private instance's"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("JacRed card lacks %q", want)
		}
	}
	if strings.Contains(out, "needs no key") {
		t.Error("the card still says jacred.su needs no key")
	}
}
