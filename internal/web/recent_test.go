package web_test

import (
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/web"
)

func postRecent(t *testing.T, h http.Handler, body string, session *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/ts/recent", strings.NewReader(body))
	req.Header.Set("Datastar-Request", "true")
	req.Header.Set("Content-Type", "application/json")
	if session != nil {
		req.AddCookie(sessionCookie(session))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestTheHomePageAsksTheBrowserForTheTorrServersLatestTorrents(t *testing.T) {
	g := newGoogle(t)
	h := web.New(withTMDB(t, g.config()))
	body := html.UnescapeString(getWith(t, h, "/", signIn(t, h)).Body.String())
	for _, want := range []string{`id="recent-torrents"`, "tsList(el, $_tsRecentUrl)", "@post('/api/ts/recent'"} {
		if !strings.Contains(body, want) {
			t.Errorf("the home page lacks %q", want)
		}
	}
}

func TestTheTenTorrentsAddedLastAreACarouselNewestFirst(t *testing.T) {
	h, session, _ := torrServers(t)
	var torrents []string
	for i := 1; i <= 12; i++ {
		torrents = append(torrents, fmt.Sprintf(`{"hash":"%040d","title":"Torrent %d","poster":"https://image.tmdb.org/t/p/w500/p%d.jpg","timestamp":%d,"stat":3,"stat_string":"Torrent working"}`, i, i, i, 1_700_000_000+i))
	}
	res := postRecent(t, h, `{"torrents":[`+strings.Join(torrents, ",")+`]}`, session)
	body := res.Body.String()
	if res.Code != http.StatusOK || !strings.Contains(body, "Recently added to TorrServer") || !strings.Contains(body, `href="/torrserver"`) || !strings.Contains(body, `class="carousel `) {
		t.Fatalf("POST /api/ts/recent = %d, want a row leading to TorrServer:\n%s", res.Code, body)
	}
	var got []string
	for _, m := range regexp.MustCompile(`>(Torrent \d+)<`).FindAllStringSubmatch(body, -1) {
		got = append(got, m[1])
	}
	if want := "Torrent 12 Torrent 11 Torrent 10 Torrent 9 Torrent 8 Torrent 7 Torrent 6 Torrent 5 Torrent 4 Torrent 3"; strings.Join(got, " ") != want {
		t.Errorf("the row shows %v, want the ten added last, newest first: %s", got, want)
	}
	if strings.Contains(postRecent(t, h, `{"torrents":[]}`, session).Body.String(), "Recently added") {
		t.Error("with no torrents there is no row")
	}
	if res := postRecent(t, h, `{"torrents":[]}`, nil); res.Code != http.StatusUnauthorized {
		t.Errorf("signed out = %d, want 401", res.Code)
	}
}
