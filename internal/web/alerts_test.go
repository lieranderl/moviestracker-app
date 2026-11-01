package web_test

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/releases"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/web"
)

// alerts is a signed-in session in an app whose backend found Dune in
// Dolby Vision and Arrival in HDR10.
func alerts(t *testing.T) (h http.Handler, session *httptest.ResponseRecorder, users *store.Memory) {
	t.Helper()
	g := newGoogle(t)
	cfg := withTMDB(t, g.config())
	users = store.NewMemory()
	cfg.Store = users
	cfg.Releases = releases.NewMemory(map[releases.Feed][]releases.Release{
		releases.DolbyVision: {{ID: 438631, Title: "Dune", PosterPath: "/d.jpg", FoundAt: now}},
		releases.HDR10:       {{ID: 329865, Title: "Arrival", PosterPath: "/a.jpg", FoundAt: now}},
	})
	h = web.New(cfg)
	return h, signIn(t, h), users
}

func TestAUserHearsWhenAFavouriteComesOutInDolbyVision(t *testing.T) {
	h, session, _ := alerts(t)
	if body := send(t, h, http.MethodGet, "/api/alerts/count", session).Body.String(); strings.Contains(body, "badge") {
		t.Errorf("before following anything the bell shows a count:\n%s", body)
	}
	send(t, h, http.MethodPost, "/api/favorites?type=movie&id=438631", session)
	bell := html.UnescapeString(send(t, h, http.MethodGet, "/api/alerts/count", session).Body.String())
	if !strings.Contains(bell, `id="alerts-bell"`) || !strings.Contains(bell, `<span class="indicator-item badge badge-primary badge-xs">1</span>`) {
		t.Fatalf("after Dune turned up the bell is:\n%s", bell)
	}
	page := html.UnescapeString(getWith(t, h, "/alerts", session).Body.String())
	for _, want := range []string{"Dune", "Dolby Vision", `href="/movie/438631#sources"`} {
		if !strings.Contains(page, want) {
			t.Errorf("alerts page lacks %q", want)
		}
	}
	if body := send(t, h, http.MethodGet, "/api/alerts/count", session).Body.String(); strings.Contains(body, "badge") {
		t.Errorf("after opening the updates the bell still counts:\n%s", body)
	}
}

func TestWatchedMoviesBringNoAlerts(t *testing.T) {
	h, session, users := alerts(t)
	ctx := context.Background()
	send(t, h, http.MethodPost, "/api/titles/state?type=movie&id=329865&state=want", session)
	send(t, h, http.MethodPost, "/api/titles/state?type=movie&id=329865&state=watched", session)
	send(t, h, http.MethodGet, "/api/alerts/count", session)
	if got, _, _ := users.Updates(ctx, "1098765", ""); len(got) != 0 {
		t.Errorf("updates for a watched movie: %+v", got)
	}
	send(t, h, http.MethodPost, "/api/titles/state?type=movie&id=329865&state=want", session)
	if body := send(t, h, http.MethodGet, "/api/alerts/count", session).Body.String(); !strings.Contains(body, ">1</span>") {
		t.Errorf("Arrival wanted again in HDR10 brings no update:\n%s", body)
	}
}

func TestTheNavbarAsksForTheAlertsCount(t *testing.T) {
	h, session, _ := alerts(t)
	if page := html.UnescapeString(getWith(t, h, "/search", session).Body.String()); !strings.Contains(page, "@get('/api/alerts/count')") {
		t.Error("the navbar does not ask for the updates count")
	}
}

func TestAWatchedSeriesDoesNotSilenceAMovieWithTheSameID(t *testing.T) {
	h, session, users := alerts(t)
	if err := users.SetTitleState(context.Background(), "1098765", store.TitleState{Kind: "tv", TMDBID: 438631, Title: "A series", State: store.Watched}); err != nil {
		t.Fatal(err)
	}
	send(t, h, http.MethodPost, "/api/favorites?type=movie&id=438631", session)
	if body := send(t, h, http.MethodGet, "/api/alerts/count", session).Body.String(); !strings.Contains(body, ">1</span>") {
		t.Errorf("a watched series with Dune's ID silenced Dune's alert:\n%s", body)
	}
}

// failingAlerts is a store that cannot read release alerts right now.
type failingAlerts struct{ *store.Memory }

func (failingAlerts) Updates(context.Context, string, string) ([]store.Update, string, error) {
	return nil, "", errors.New("firestore is unavailable")
}

func TestAlertsStayUnseenWhenTheyCannotBeLoaded(t *testing.T) {
	g := newGoogle(t)
	cfg := withTMDB(t, g.config())
	users := store.NewMemory()
	if err := users.AddUpdates(context.Background(), "1098765", []store.Update{{Feed: "dvmovies", TMDBID: 438631, Title: "Dune"}}); err != nil {
		t.Fatal(err)
	}
	cfg.Store = failingAlerts{users}
	h := web.New(cfg)
	session := signIn(t, h)
	res := getWith(t, h, "/alerts", session)
	if res.Code != http.StatusServiceUnavailable || !strings.Contains(res.Body.String(), "could not be loaded") {
		t.Errorf("/alerts = %d, want 503 saying the alerts could not be loaded", res.Code)
	}
	if kept, _, _ := users.Updates(context.Background(), "1098765", ""); len(kept) != 1 || kept[0].Seen {
		t.Errorf("alerts after a failed load = %+v, want Dune still unseen", kept)
	}
}

// manyAlerts keeps n unseen alerts for the signed-in user, found a minute
// apart before now, movie 1 newest.
func manyAlerts(t *testing.T, users *store.Memory, n int) {
	t.Helper()
	updates := make([]store.Update, n)
	for i := range updates {
		updates[i] = store.Update{Feed: "latesttorrentsmovies", TMDBID: i + 1, Title: fmt.Sprint("Movie ", i+1), FoundAt: now.Add(-time.Duration(i+1) * time.Minute)}
	}
	if err := users.AddUpdates(context.Background(), "1098765", updates); err != nil {
		t.Fatal(err)
	}
}

func bellCount(t *testing.T, h http.Handler, session *httptest.ResponseRecorder) string {
	t.Helper()
	bell := send(t, h, http.MethodGet, "/api/alerts/count", session).Body.String()
	m := regexp.MustCompile(`badge-xs">([^<]*)</span>`).FindStringSubmatch(bell)
	if m == nil {
		return ""
	}
	return m[1]
}

// With more alerts than a page holds, opening them marks read only those
// shown; the older ones stay unread until their page is opened.
func TestOpeningAlertsMarksReadOnlyTheAlertsShown(t *testing.T) {
	h, session, users := alerts(t)
	manyAlerts(t, users, 130)
	if got := bellCount(t, h, session); got != "99+" {
		t.Errorf("130 unread alerts show %q on the bell, want 99+", got)
	}
	page := html.UnescapeString(getWith(t, h, "/alerts", session).Body.String())
	if !strings.Contains(page, `href="/movie/1#sources"`) || !strings.Contains(page, `href="/movie/100#sources"`) || strings.Contains(page, `href="/movie/101#sources"`) {
		t.Fatal("the first page should list the newest 100 alerts")
	}
	if got := bellCount(t, h, session); got != "30" {
		t.Errorf("after the first page the bell shows %q, want the 30 not shown", got)
	}
	older := regexp.MustCompile(`href="(/alerts\?after=[^"]+)"`).FindStringSubmatch(page)
	if older == nil {
		t.Fatal("no link to older alerts")
	}
	rest := html.UnescapeString(getWith(t, h, older[1], session).Body.String())
	if !strings.Contains(rest, `href="/movie/130#sources"`) || strings.Contains(rest, `href="/movie/100#sources"`) || strings.Contains(rest, "after=") {
		t.Error("the older page should list the other 30 and no further link")
	}
	if got := bellCount(t, h, session); got != "" {
		t.Errorf("after both pages the bell shows %q", got)
	}
	if res := getWith(t, h, "/alerts?after=nonsense", session); res.Code != http.StatusBadRequest {
		t.Errorf("a malformed page link = %d, want 400", res.Code)
	}
}

// Mark all read covers the alerts there were when the page was shown: one
// arriving after that stays unread.
func TestMarkAllReadLeavesAnAlertThatArrivedLaterUnread(t *testing.T) {
	h, session, users := alerts(t)
	manyAlerts(t, users, 130)
	page := html.UnescapeString(getWith(t, h, "/alerts", session).Body.String())
	markAll := regexp.MustCompile(`@post\('(/api/alerts/seen\?through=\d+)'`).FindStringSubmatch(page)
	if markAll == nil || !strings.Contains(page, "Mark all read") {
		t.Fatal("with unread alerts left, the page offers no Mark all read")
	}
	if err := users.AddUpdates(context.Background(), "1098765", []store.Update{{Feed: "hdr10movies", TMDBID: 329865, Title: "Arrival", FoundAt: now.Add(time.Second)}}); err != nil {
		t.Fatal(err)
	}
	res := send(t, h, http.MethodPost, markAll[1], session)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `id="alerts-bell"`) || !strings.Contains(res.Body.String(), `id="alerts-mark-all"`) {
		t.Fatalf("Mark all read = %d:\n%s", res.Code, res.Body)
	}
	if got := bellCount(t, h, session); got != "1" {
		t.Errorf("after Mark all read the bell shows %q, want 1 for the later Arrival", got)
	}
	if res := send(t, h, http.MethodPost, "/api/alerts/seen?through=soon", session); res.Code != http.StatusBadRequest {
		t.Errorf("Mark all read without a time = %d, want 400", res.Code)
	}
}

// When every alert fits on the page there is nothing left to mark.
func TestAllAlertsOnOnePageNeedNoMarkAllRead(t *testing.T) {
	h, session, users := alerts(t)
	manyAlerts(t, users, 3)
	page := getWith(t, h, "/alerts", session).Body.String()
	if strings.Contains(page, "Mark all read") || strings.Contains(page, "after=") {
		t.Error("three alerts offer Mark all read or older alerts")
	}
}
