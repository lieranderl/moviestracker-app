package web_test

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/web"
)

const duneProgress = `{"hash":"aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111","index":2,"position":2530.4,"duration":9360.2,` +
	`"torrent":{"hash":"aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111","title":"Dune","poster":"https://image.tmdb.org/t/p/w300/d.jpg",` +
	`"data":"{\"tmdb\":{\"id\":438631,\"type\":\"movie\"}}","file_stats":[{"id":1,"path":"Sample.mkv"},{"id":2,"path":"Dune.2021.2160p.mkv"}]}}`

func TestThePlayerSavesWhereAUserStoppedWatching(t *testing.T) {
	h, session, users := torrServers(t)
	res := postPlayer(t, h, "/api/progress", duneProgress, session)
	if res.Code != http.StatusNoContent {
		t.Fatalf("saving progress = %d %s, want 204", res.Code, res.Body)
	}
	got, ok, err := users.Progress(context.Background(), "1098765", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 2)
	if err != nil || !ok {
		t.Fatalf("progress = %v, %v; want kept", ok, err)
	}
	if got.Position != 2530*time.Second || got.Duration != 9360*time.Second || got.File != "Dune.2021.2160p.mkv" ||
		got.Title != "Dune" || got.Poster != "https://image.tmdb.org/t/p/w300/d.jpg" || got.Kind != "movie" || got.TMDBID != 438631 {
		t.Errorf("progress = %+v, want Dune's file at 42:10 of 2:36:00, linked to TMDB movie 438631", got)
	}
}

func TestProgressIsSavedOnlyForSignedInUsersAndPlausibleTimes(t *testing.T) {
	h, session, _ := torrServers(t)
	if res := postPlayer(t, h, "/api/progress", duneProgress, nil); res.Code != http.StatusUnauthorized {
		t.Errorf("signed out = %d, want 401", res.Code)
	}
	for name, body := range map[string]string{
		"past the end": `{"hash":"aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111","index":2,"position":9999,"duration":60,"torrent":{"hash":"aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111"}}`,
		"no duration":  `{"hash":"aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111","index":2,"position":10,"duration":0,"torrent":{"hash":"aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111"}}`,
		"another hash": `{"hash":"aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111","index":2,"position":10,"duration":60,"torrent":{"hash":"bbbb1111aaaa1111aaaa1111aaaa1111aaaa1111"}}`,
		"not a hash":   `{"hash":"dune","index":2,"position":10,"duration":60,"torrent":{"hash":"dune"}}`,
	} {
		if res := postPlayer(t, h, "/api/progress", body, session); res.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", name, res.Code)
		}
	}
}

func TestTheWebPlayerReportsProgressWithTheRelayedTorrentAndOffersToResume(t *testing.T) {
	h, session, _ := torrServers(t)
	page := html.UnescapeString(getWith(t, h, "/torrserver", session).Body.String())
	for _, want := range []string{"@post('/api/progress'", "torrent: $_progressTorrent", "/api/progress/resume?hash=", `id="torr-resume"`} {
		if !strings.Contains(page, want) {
			t.Errorf("player lacks %q", want)
		}
	}
	postPlayer(t, h, "/api/progress", duneProgress, session)
	res := send(t, h, http.MethodGet, "/api/progress/resume?hash=aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111&index=2", session)
	if !strings.Contains(res.Body.String(), "Resume from 42:10") {
		t.Errorf("resume = %d, want an offer to resume from 42:10:\n%s", res.Code, res.Body)
	}
}

func TestTheHomePageListsWhatAUserIsWatching(t *testing.T) {
	h, session, _ := favorites(t)
	if page := getWith(t, h, "/", session).Body.String(); strings.Contains(page, `id="continue-watching"`) {
		t.Error("a user who never played anything sees Continue watching")
	}
	postPlayer(t, h, "/api/progress", duneProgress, session)
	page := html.UnescapeString(getWith(t, h, "/", session).Body.String())
	for _, want := range []string{`id="continue-watching"`, "Continue watching", `href="/torrserver?play=aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111&file=2"`, `value="27"`, "Dune", "1:53:50 left"} {
		if !strings.Contains(page, want) {
			t.Errorf("home page lacks %q", want)
		}
	}
}

func TestContinueWatchingOpensTheWebPlayerOnceTheTorrServerAnswers(t *testing.T) {
	h, session, _ := torrServers(t)
	postPlayer(t, h, "/api/progress", duneProgress, session)
	page := html.UnescapeString(getWith(t, h, "/torrserver?play=aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111&file=2", session).Body.String())
	for _, want := range []string{`id="torr-autoplay"`, "$tsSelected && $tsStatus.ok", "$activeHash = 'aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111'", "$activeFileIndex = 2", `$streamTitle = "Dune"`} {
		if !strings.Contains(page, want) {
			t.Errorf("player lacks %q", want)
		}
	}
}

func TestAWebTitlePageAsksWhereTheUserStopped(t *testing.T) {
	h, session, _ := favorites(t)
	page := html.UnescapeString(getWith(t, h, "/movie/438631", session).Body.String())
	if !strings.Contains(page, "@get('/api/progress/title?type=movie&id=438631')") {
		t.Error("the movie page does not ask where the user stopped")
	}
	postPlayer(t, h, "/api/progress", duneProgress, session)
	if body := send(t, h, http.MethodGet, "/api/progress/title?type=movie&id=438631", session).Body.String(); !strings.Contains(body, "42:10 of 2:36:00") {
		t.Errorf("title progress = %s, want Continue at 42:10", body)
	}
}

func TestTheWebPlayerSharesTimecodesWithTheUsersTorrServer(t *testing.T) {
	h, session, _ := torrServers(t)
	page := html.UnescapeString(getWith(t, h, "/torrserver", session).Body.String())
	for _, want := range []string{"tsViewedSet($tsSelected, $activeHash, +$activeFileIndex, $_time)", "tsTimecode($tsSelected, $activeHash, +$activeFileIndex).then(at =>", "&at=' + at"} {
		if !strings.Contains(page, want) {
			t.Errorf("player lacks %q", want)
		}
	}
	res := send(t, h, http.MethodGet, "/api/progress/resume?hash=aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111&index=1&at=600", session)
	if !strings.Contains(res.Body.String(), "Resume from 10:00") {
		t.Errorf("resume = %s, want TorrServer's 10:00 when Moviestracker has none", res.Body)
	}
}

func TestAUserCanRemoveAContinueWatchingEntryWithoutChangingTheirLibrary(t *testing.T) {
	h, session, users := favorites(t)
	ctx := context.Background()
	if err := users.AddFavorite(ctx, "1098765", store.Favorite{Kind: "movie", TMDBID: 438631, Title: "Dune"}); err != nil {
		t.Fatal(err)
	}
	if err := users.SetTitleState(ctx, "1098765", store.TitleState{Kind: "movie", TMDBID: 438631, Title: "Dune", State: store.Watched}); err != nil {
		t.Fatal(err)
	}
	if err := users.MarkEpisodes(ctx, "1098765", 95396, 1, []int{1}, true); err != nil {
		t.Fatal(err)
	}
	if err := users.SaveReview(ctx, "1098765", store.Review{Kind: "movie", TMDBID: 438631, Rating: 9}); err != nil {
		t.Fatal(err)
	}
	postPlayer(t, h, "/api/progress", duneProgress, session)
	before, err := users.ExportAccount(ctx, "1098765")
	if err != nil {
		t.Fatal(err)
	}
	page := html.UnescapeString(getWith(t, h, "/", session).Body.String())
	if !strings.Contains(page, "Remove Dune from Continue watching") {
		t.Error("Continue watching has no labelled remove action")
	}
	res := send(t, h, http.MethodDelete, "/api/progress?hash=aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111&index=2", session)
	if res.Code != http.StatusOK || !strings.Contains(res.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("remove = %d %s, want SSE success", res.Code, res.Body)
	}
	if strings.Contains(res.Body.String(), "continue-watching-title") {
		t.Error("removing the final entry kept the row heading")
	}
	if !strings.Contains(res.Body.String(), "Removed from Continue watching") {
		t.Error("removing progress was not announced")
	}
	if page := getWith(t, h, "/", session).Body.String(); strings.Contains(page, "continue-watching-title") {
		t.Error("removed entry returns on page reload")
	}
	resume := send(t, h, http.MethodGet, "/api/progress/resume?hash=aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111&index=2&at=600", session)
	if resume.Code != http.StatusOK || !strings.Contains(resume.Body.String(), `"_resumeAt":0`) {
		t.Errorf("external timecode restored reset progress: %d %s", resume.Code, resume.Body)
	}
	after, err := users.ExportAccount(ctx, "1098765")
	if err != nil {
		t.Fatal(err)
	}
	before.Progress = nil
	after.Progress = nil
	if !reflect.DeepEqual(before, after) {
		t.Errorf("reset changed non-progress account data: before %+v, after %+v", before, after)
	}
}

func TestProgressRemovalRequiresASignedInUserAndValidFile(t *testing.T) {
	h, session, users := torrServers(t)
	postPlayer(t, h, "/api/progress", duneProgress, session)
	signedOut := httptest.NewRecorder()
	h.ServeHTTP(signedOut, httptest.NewRequest(http.MethodDelete, "/api/progress?hash=aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111&index=2", nil))
	if signedOut.Code != http.StatusUnauthorized {
		t.Errorf("signed-out reset = %d, want 401", signedOut.Code)
	}
	for _, path := range []string{"/api/progress", "/api/progress?hash=invalid&index=2", "/api/progress?hash=aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111&index=0"} {
		if res := send(t, h, http.MethodDelete, path, session); res.Code != http.StatusBadRequest {
			t.Errorf("reset %s = %d, want 400", path, res.Code)
		}
	}
	got, _, err := users.Progress(context.Background(), "1098765", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 2)
	if err != nil || got.Position != 2530*time.Second {
		t.Errorf("invalid requests changed progress: %+v, %v", got, err)
	}
}

func TestRemovingOneContinueWatchingEntryKeepsOtherCards(t *testing.T) {
	h, session, users := torrServers(t)
	postPlayer(t, h, "/api/progress", duneProgress, session)
	other := store.Progress{Hash: "bbbb1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 1, Title: "Arrival", Position: 5 * time.Minute, Duration: 2 * time.Hour}
	if err := users.SaveProgress(context.Background(), "1098765", other); err != nil {
		t.Fatal(err)
	}
	path := "/api/progress?hash=aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111&index=2"
	for range 2 {
		res := send(t, h, http.MethodDelete, path, session)
		if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "Remove Arrival from Continue watching") || strings.Contains(res.Body.String(), "Remove Dune from Continue watching") {
			t.Errorf("reset response lost another card or retained Dune: %d %s", res.Code, res.Body)
		}
		if res.Result().Header.Get("Cache-Control") != "no-store" {
			t.Error("personal progress response can be cached")
		}
	}
}

type unavailableProgressStore struct {
	store.Store
	resetErr, errorRead error
}

func (s *unavailableProgressStore) ResetProgress(ctx context.Context, uid, hash string, index int) error {
	if s.resetErr != nil {
		return s.resetErr
	}
	return s.Store.ResetProgress(ctx, uid, hash, index)
}
func (s *unavailableProgressStore) RecentProgress(ctx context.Context, uid string, limit int) ([]store.Progress, error) {
	if s.errorRead != nil {
		return nil, s.errorRead
	}
	return s.Store.RecentProgress(ctx, uid, limit)
}

func TestProgressRemovalFailuresExplainHowToRetry(t *testing.T) {
	for _, readFails := range []bool{false, true} {
		t.Run(fmt.Sprintf("read=%t", readFails), func(t *testing.T) {
			g := newGoogle(t)
			users := store.NewMemory()
			boundary := &unavailableProgressStore{Store: users}
			if readFails {
				boundary.errorRead = errors.New("database unavailable")
			} else {
				boundary.resetErr = errors.New("database unavailable")
			}
			cfg := withTMDB(t, g.config())
			cfg.Store = boundary
			h := web.New(cfg)
			session := signIn(t, h)
			postPlayer(t, h, "/api/progress", duneProgress, session)
			path := "/api/progress?hash=aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111&index=2"
			res := send(t, h, http.MethodDelete, path, session)
			if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "Continue watching could not be updated. Please try again.") || strings.Contains(res.Body.String(), "Removed from Continue watching.") {
				t.Errorf("failure lacks retry feedback: %d %s", res.Code, res.Body)
			}
			boundary.resetErr, boundary.errorRead = nil, nil
			retry := send(t, h, http.MethodDelete, path, session)
			if retry.Code != http.StatusOK || !strings.Contains(retry.Body.String(), "Removed from Continue watching.") {
				t.Errorf("retry did not recover: %d %s", retry.Code, retry.Body)
			}
		})
	}
}
