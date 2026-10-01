package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAUserCanChooseATorrentsFilesForDirectOrHLSPlayback(t *testing.T) {
	h, session, _ := torrServers(t)
	page := getWith(t, h, "/torrserver", session)
	for _, want := range []string{`id="torr-player-modal"`, `/static/player.js`, `tsPlayerLoad`, `$_tsPlayerProblem = evt.detail.problem || evt.detail.heartbeatProblem`, `$_queueHash !== $activeHash`, `$audioTrack &lt; 0`} {
		if !strings.Contains(page.Body.String(), want) {
			t.Errorf("player page lacks %q", want)
		}
	}
	res := postTorrents(t, h, twoTorrents, session)
	for _, want := range []string{"Dune.2021.mkv", `data-action="play"`, `data-action="files"`, `data-kind="hls"`, `data-index="1"`, `tsPlayerFiles(el.closest(&#39;#ts-torrents-box&#39;)`} {
		if !strings.Contains(res.Body.String(), want) {
			t.Errorf("torrent cards lack %q", want)
		}
	}
	if res.Code != http.StatusOK {
		t.Fatalf("cards status %d", res.Code)
	}
}

func postPlayer(t *testing.T, h http.Handler, endpoint, body string, session *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if session != nil {
		req.AddCookie(sessionCookie(session))
	}
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	return res
}

func TestBrowserRelayedPlayerDataRendersTracksAndStatsWithoutReachingTorrServer(t *testing.T) {
	h, session, _ := torrServers(t)
	body := `{"hash":"aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111","index":1,"kind":"hls","nonce":1,"torrent":{"hash":"aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111","active_peers":12,"total_peers":40,"file_stats":[{"id":1,"path":"Sintel.mp4"},{"id":2,"path":"Next.mp4"}]},"probe":{"Tracks":[{"Index":0,"Type":"audio","Language":"en"},{"Index":1,"Type":"audio","Language":"ru"},{"Index":0,"Type":"sub","Language":"en"}]}}`
	res := postPlayer(t, h, "/api/ts/player", body, session)
	if res.Code != 200 {
		t.Fatalf("relay = %d %s", res.Code, res.Body)
	}
	for _, want := range []string{`id="torr-audio-picker"`, `id="torr-subtitle-menu"`, `id="torr-player-stats"`, "Peers 12/40", `data-track="1"`, "Next.mp4"} {
		if !strings.Contains(res.Body.String(), want) {
			t.Errorf("relay lacks %q", want)
		}
	}
	if strings.Contains(res.Body.String(), "Buffer 0%") {
		t.Fatal("unknown cache capacity presented as an empty buffer")
	}
}

func TestPlayerRelaysRequireSignInAndRefuseInvalidOrOversizedMedia(t *testing.T) {
	h, session, _ := torrServers(t)
	good := `{"hash":"aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111","index":1,"kind":"direct","torrent":{"hash":"aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111"}}`
	for _, endpoint := range []string{"/api/ts/player", "/api/ts/player-stats", "/api/ts/files"} {
		if res := postPlayer(t, h, endpoint, good, nil); res.Code != 401 {
			t.Errorf("%s signed out = %d", endpoint, res.Code)
		}
		if res := postPlayer(t, h, endpoint, strings.Replace(good, `"index":1`, `"index":-1`, 1), session); res.Code != 400 {
			t.Errorf("%s invalid index = %d", endpoint, res.Code)
		}
		if res := postPlayer(t, h, endpoint, strings.Replace(good, `"torrent":{`, `"probe":{"Tracks":[`+strings.TrimSuffix(strings.Repeat(`{},`, 257), ",")+`]},"torrent":{`, 1), session); res.Code != 413 {
			t.Errorf("%s 257 tracks = %d", endpoint, res.Code)
		}
	}
}
