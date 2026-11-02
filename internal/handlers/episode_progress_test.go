package handlers

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

// seriesServer is a local app whose TorrServer holds a season of Severance,
// added from its title page: one file names its episode, one does not.
func seriesServer(t *testing.T) *Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/echo":
			_, _ = w.Write([]byte("MatriX.146"))
		case "/torrents":
			_, _ = w.Write([]byte(`[{"hash":"` + duneHash + `","title":"Severance S01","file_stats":[` +
				`{"id":1,"path":"Severance.S01/Severance.S01E02.1080p.mkv"},{"id":2,"path":"Severance.S01/Episode 3.mkv"}]}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	cfg := validTestConfig(t)
	cfg.TorrServer = torrserver.NewManager(ts.URL)
	s := newTestServerWithConfig(t, cfg)
	s.rememberTitle(duneHash, "tv", 95396)
	return s
}

// Finishing a file that names its episode marks that episode watched (and
// not the series); a file whose episode is uncertain is left to the user.
func TestFinishingAnEpisodeFileMarksThatEpisodeWatched(t *testing.T) {
	s := seriesServer(t)
	postProgress(t, s, `{"hash":"`+duneHash+`","index":1,"position":3000,"duration":3100}`, true)
	postProgress(t, s, `{"hash":"`+duneHash+`","index":2,"position":3000,"duration":3100}`, true)
	kept, err := store.OpenEpisodeFile(filepath.Join(s.store.Dir(), "episodes.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := kept.WatchedEpisodes(context.Background(), testUsername, 95396)
	if !reflect.DeepEqual(got, map[int][]int{1: {2}}) {
		t.Errorf("watched episodes = %v, want only S01E02", got)
	}
	if page := html.UnescapeString(get(t, s, "/api/titles/state?type=tv&id=95396", true).Body.String()); strings.Contains(page, `data-state="watched" aria-pressed="true"`) {
		t.Error("finishing one episode marked the whole series watched")
	}
}
