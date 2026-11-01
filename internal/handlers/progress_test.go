package handlers

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

const duneHash = "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111"

// progressServer is a local app whose TorrServer lists Dune, added from
// its title page.
func progressServer(t *testing.T) *Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/echo" {
			_, _ = w.Write([]byte("MatriX.146"))
			return
		}
		if r.URL.Path == "/torrents" {
			_, _ = w.Write([]byte(`[{"hash":"` + duneHash + `","title":"Dune","poster":"https://image.tmdb.org/t/p/w300/d.jpg",` +
				`"file_stats":[{"id":1,"path":"Sample.mkv"},{"id":2,"path":"Dune.2021.2160p.mkv"}]}]`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(ts.Close)
	cfg := validTestConfig(t)
	cfg.TorrServer = torrserver.NewManager(ts.URL)
	s := newTestServerWithConfig(t, cfg)
	s.rememberTitle(duneHash, "movie", 438631)
	return s
}

func postProgress(t *testing.T, s *Server, body string, signedInUser bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/progress", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if signedInUser {
		req.AddCookie(signedIn(t, s))
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func TestTheLocalPlayerSavesWhereAnAccountStoppedWatching(t *testing.T) {
	s := progressServer(t)
	res := postProgress(t, s, `{"hash":"`+duneHash+`","index":2,"position":2530.4,"duration":9360.2}`, true)
	if res.Code != http.StatusNoContent {
		t.Fatalf("saving progress = %d %s, want 204", res.Code, res.Body)
	}
	kept, err := store.OpenProgressFile(filepath.Join(s.store.Dir(), "progress.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := kept.Progress(context.Background(), testUsername, duneHash, 2)
	if err != nil || !ok {
		t.Fatalf("progress = %v, %v; want kept in the data directory", ok, err)
	}
	if got.Position != 2530*time.Second || got.File != "Dune.2021.2160p.mkv" || got.Title != "Dune" || got.Kind != "movie" || got.TMDBID != 438631 {
		t.Errorf("progress = %+v, want Dune's file at 42:10, linked to TMDB movie 438631", got)
	}
	if res := postProgress(t, s, `{"hash":"`+duneHash+`","index":2,"position":10,"duration":60}`, false); res.Code != http.StatusUnauthorized {
		t.Errorf("signed out = %d, want 401", res.Code)
	}
}

func getResume(t *testing.T, s *Server, index int) string {
	t.Helper()
	res := get(t, s, "/api/progress/resume?hash="+duneHash+"&index="+strconv.Itoa(index), true)
	if res.Code != http.StatusOK {
		t.Fatalf("resume = %d %s", res.Code, res.Body)
	}
	return res.Body.String()
}

func TestTheLocalPlayerOffersToResumeWhereTheAccountStopped(t *testing.T) {
	s := progressServer(t)
	postProgress(t, s, `{"hash":"`+duneHash+`","index":2,"position":2530.4,"duration":9360.2}`, true)
	body := getResume(t, s, 2)
	for _, want := range []string{`id="torr-resume"`, "Resume from 42:10", `$_video.currentTime = 2530`} {
		if !strings.Contains(body, want) {
			t.Errorf("resume patch lacks %q:\n%s", want, body)
		}
	}
	if body := getResume(t, s, 1); strings.Contains(body, "Resume from") {
		t.Error("a file never played offers to resume")
	}
	postProgress(t, s, `{"hash":"`+duneHash+`","index":2,"position":9300,"duration":9360.2}`, true)
	if body := getResume(t, s, 2); strings.Contains(body, "Resume from") {
		t.Error("a finished file offers to resume")
	}
}

func TestTheLocalPlayerReportsProgressAndAsksWhereToResume(t *testing.T) {
	page := html.UnescapeString(get(t, progressServer(t), "/torrserver", true).Body.String())
	for _, want := range []string{"@post('/api/progress'", "/api/progress/resume?hash=", `id="torr-resume"`} {
		if !strings.Contains(page, want) {
			t.Errorf("player lacks %q", want)
		}
	}
}
