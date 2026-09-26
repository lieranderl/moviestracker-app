package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The player's playlist is the torrent's videos, in order: the menu to jump
// between them and $_queue, which Next and the end of a file follow.
func TestThePlayerGetsTheTorrentsVideosAsItsPlaylist(t *testing.T) {
	season := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/torrents" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[{"hash":"56f590f136968628d49fe8047c9664f01296eeb1","title":"Show","file_stats":[` +
			`{"id":1,"path":"Show/e01.mkv","length":1000},{"id":2,"path":"Show/e01.srt","length":10},{"id":3,"path":"Show/e02.mkv","length":1000}]}]`))
	})
	srv, sessions, ts := setupTestServerWithTorrServer(t, season)
	defer ts.Close()
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, signedIn(t, sessions, httptest.NewRequest(http.MethodGet, "/api/torrserver/queue?hash=56f590f136968628d49fe8047c9664f01296eeb1", nil)))
	body := rr.Body.String()
	for _, want := range []string{`"_queueHash":"56f590f136968628d49fe8047c9664f01296eeb1"`, `"index":1`, `"index":3`, `"title":"Show/e02.mkv"`, `id="torr-playlist"`, `data-at="1"`} {
		if !strings.Contains(body, want) {
			t.Errorf("playlist lacks %s:\n%s", want, body)
		}
	}
	if strings.Contains(body, `"index":2`) {
		t.Errorf("subtitles are not in the playlist:\n%s", body)
	}
}

func TestAnUnknownTorrentHasNoPlaylist(t *testing.T) {
	srv, sessions, ts := setupTestServerWithTorrServer(t, http.NotFoundHandler())
	defer ts.Close()
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, signedIn(t, sessions, httptest.NewRequest(http.MethodGet, "/api/torrserver/queue?hash=56f590f136968628d49fe8047c9664f01296eeb1", nil)))
	if body := rr.Body.String(); !strings.Contains(body, `"_queue":[]`) || strings.Contains(body, "data-at=") {
		t.Errorf("want an empty playlist, got:\n%s", body)
	}
}
