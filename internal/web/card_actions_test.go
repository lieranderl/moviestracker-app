package web_test

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A signed-in visitor marks a card's title Want to watch or Watched without
// leaving the page; whoever is signed out sees no such buttons.
func TestOnlySignedInVisitorsGetQuickActionsOnCards(t *testing.T) {
	h, session, users := favorites(t)
	l, err := users.CreateCustomList(context.Background(), "1098765", "Sci-fi nights")
	if err != nil {
		t.Fatal(err)
	}
	send(t, h, http.MethodPost, "/api/lists/"+l.ID+"/titles?type=movie&id=438631", session)
	page := html.UnescapeString(getWith(t, h, "/lists/c/"+l.ID, session).Body.String())
	for _, want := range []string{
		"@post('/api/titles/quick?type=movie&id=438631&state=want')",
		"@post('/api/titles/quick?type=movie&id=438631&state=watched')",
		`aria-label="Add Dune to Want to watch"`,
		"@post($_quickUndo)",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("a signed-in card lacks %q", want)
		}
	}
	shared := html.UnescapeString(send(t, h, http.MethodPost, "/api/lists/"+l.ID+"/share", session).Body.String())
	link := shared[strings.Index(shared, "/l/"):]
	link = link[:strings.IndexAny(link, `"'< `)]
	out := html.UnescapeString(getWith(t, h, link, httptest.NewRecorder()).Body.String())
	if !strings.Contains(out, `href="/movie/438631"`) {
		t.Fatalf("the shared list %s does not show Dune", link)
	}
	if strings.Contains(out, "/api/titles/quick") || strings.Contains(out, "_quickUndo") {
		t.Error("a signed-out visitor gets quick actions")
	}
}
