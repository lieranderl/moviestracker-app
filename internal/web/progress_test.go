package web_test

import (
	"context"
	"html"
	"net/http"
	"strings"
	"testing"
	"time"
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
