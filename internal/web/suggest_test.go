package web_test

import (
	"html"
	"net/http"
	"strings"
	"testing"
)

func TestTheNavbarSuggestsTitlesAsTheUserTypes(t *testing.T) {
	h, session, _ := favorites(t)
	page := html.UnescapeString(getWith(t, h, "/search", session).Body.String())
	if !strings.Contains(page, "@get('/api/search/suggest', {filterSignals: {include: /^navQ$/}})") || !strings.Contains(page, `id="search-suggest"`) {
		t.Fatal("the navbar search does not ask for suggestions")
	}
	body := html.UnescapeString(sendSignals(t, h, http.MethodGet, "/api/search/suggest", `{"navQ":"dune"}`, session).Body.String())
	for _, want := range []string{`id="search-suggest"`, `href="/movie/438631"`, "Dune", `href="/search?q=dune"`} {
		if !strings.Contains(body, want) {
			t.Errorf("suggestions for dune lack %q:\n%s", want, body)
		}
	}
	if body := sendSignals(t, h, http.MethodGet, "/api/search/suggest", `{"navQ":"  "}`, session).Body.String(); strings.Contains(body, "href=") {
		t.Errorf("a blank query suggests something:\n%s", body)
	}
}
