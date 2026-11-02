package views_test

import (
	"context"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// On the web app a release's send button names the TorrServer it goes to,
// following the selector; the full name is in its tooltip.
func TestTheSendButtonNamesTheSelectedTorrServer(t *testing.T) {
	ctx := views.WithSite(context.Background(), views.Site{Cloud: true})
	out := html.UnescapeString(renderIn(t, ctx, views.TorrentResults(releases(), "seeders", "", false)))
	send := regexp.MustCompile(`(?s)<button[^>]*data-action="send".*?</button>`).FindString(out)
	for _, want := range []string{`data-attr:title=`, `data-attr:aria-label=`, `'Add to %s'.replace('%s', $_tsSelectedName)`, `data-text=`, "truncate"} {
		if !strings.Contains(send, want) {
			t.Errorf("send button lacks %q:\n%s", want, send)
		}
	}
	selector := html.UnescapeString(renderIn(t, ctx, views.WebSourceSelector([]store.TorrServer{{Name: "Living room", URL: "http://tv:8090"}, {Name: "Home", URL: "http://nas:8090"}})))
	if !strings.Contains(selector, `data-server-names="{"http://nas:8090":"Home","http://tv:8090":"Living room"}"`) || !strings.Contains(selector, "$_tsSelectedName = JSON.parse(el.dataset.serverNames)[$tsSelected] || ''") {
		t.Errorf("selector does not keep the selected server's name:\n%s", selector)
	}
}

// The local app has one TorrServer: its button keeps the plain label.
func TestTheLocalSendButtonSaysTorrServer(t *testing.T) {
	out := render(t, views.TorrentResults(releases(), "seeders", "", false))
	if strings.Contains(out, "_tsSelectedName") || !strings.Contains(out, `aria-label="Send to TorrServer"`) {
		t.Error("the local app's send button should say TorrServer, with an accessible name")
	}
}
