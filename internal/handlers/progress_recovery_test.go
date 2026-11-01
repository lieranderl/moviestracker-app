package handlers

import (
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

// goneServer is a local app whose user watched part of Dune's file 2,
// added from Dune's title page; gone then empties its TorrServer.
func goneServer(t *testing.T) (*Server, *atomic.Bool) {
	t.Helper()
	var gone atomic.Bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/echo":
			_, _ = w.Write([]byte("MatriX.146"))
		case r.URL.Path == "/torrents" && gone.Load():
			_, _ = w.Write([]byte(`[]`))
		case r.URL.Path == "/torrents":
			_, _ = w.Write([]byte(`[{"hash":"` + duneHash + `","title":"Dune","file_stats":[{"id":2,"path":"Dune.2021.2160p.mkv"}]}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	cfg := validTestConfig(t)
	cfg.TorrServer = torrserver.NewManager(ts.URL)
	s := newTestServerWithConfig(t, cfg)
	s.rememberTitle(duneHash, "movie", 438631)
	postProgress(t, s, `{"hash":"`+duneHash+`","index":2,"position":2530.4,"duration":9360.2}`, true)
	return s, &gone
}

// Continuing a file TorrServer no longer has explains it and leads back to
// the title's sources, instead of opening a player with nothing to play.
func TestContinuingAFileTorrServerNoLongerHasLeadsBackToItsSources(t *testing.T) {
	s, gone := goneServer(t)
	gone.Store(true)
	page := html.UnescapeString(get(t, s, "/torrserver?play="+duneHash+"&file=2", true).Body.String())
	if strings.Contains(page, `id="torr-autoplay"`) {
		t.Error("the player opens on a file TorrServer does not have")
	}
	for _, want := range []string{`id="resume-missing"`, "is no longer on your TorrServer", `href="/movie/438631#sources"`, "Dune"} {
		if !strings.Contains(page, want) {
			t.Errorf("the TorrServer page lacks %q", want)
		}
	}
}

// A file never kept by title is looked for by its name.
func TestContinuingAnUnknownMissingFileSearchesForIt(t *testing.T) {
	s, gone := goneServer(t)
	gone.Store(true)
	page := html.UnescapeString(get(t, s, "/torrserver?play="+duneHash+"&file=5", true).Body.String())
	if strings.Contains(page, `id="torr-autoplay"`) || !strings.Contains(page, `id="resume-missing"`) || !strings.Contains(page, `href="/search"`) {
		t.Error("a missing file with no title should offer search, not the player")
	}
}
