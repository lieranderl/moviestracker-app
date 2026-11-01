package web_test

import (
	"context"
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	if got, _ := users.Updates(ctx, "1098765"); len(got) != 0 {
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

func (failingAlerts) Updates(context.Context, string) ([]store.Update, error) {
	return nil, errors.New("firestore is unavailable")
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
	if kept, _ := users.Updates(context.Background(), "1098765"); len(kept) != 1 || kept[0].Seen {
		t.Errorf("alerts after a failed load = %+v, want Dune still unseen", kept)
	}
}
