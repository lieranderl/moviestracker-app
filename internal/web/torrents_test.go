package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// twoTorrents is TorrServer's /torrents list answer, as the browser posts it.
const twoTorrents = `{"torrents":[
	{"hash":"aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111","title":"Dune (2021)","poster":"https://image.tmdb.org/t/p/w500/d.jpg","torrent_size":8589934592,"stat":3,"stat_string":"Torrent working","download_speed":1048576,"active_peers":12,"total_peers":40,
	 "file_stats":[{"id":1,"path":"Dune/Dune.2021.mkv","length":8589000000}]},
	{"hash":"bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222","name":"Severance.S01","torrent_size":21474836480,"stat":5,"stat_string":"Torrent in db"}
]}`

func postTorrents(t *testing.T, h http.Handler, body string, session *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/ts/torrents", strings.NewReader(body))
	req.Header.Set("Datastar-Request", "true")
	req.Header.Set("Content-Type", "application/json")
	if session != nil {
		req.AddCookie(sessionCookie(session))
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestTheBrowsersTorrentListIsShownAsCards(t *testing.T) {
	h, session, _ := torrServers(t)
	res := postTorrents(t, h, twoTorrents, session)
	body := res.Body.String()
	if res.Code != http.StatusOK || !strings.Contains(body, `id="ts-torrents"`) {
		t.Fatalf("POST /api/ts/torrents = %d, want #ts-torrents patched:\n%s", res.Code, body)
	}
	for _, want := range []string{"Dune (2021)", "Severance.S01", "8.00 GB", "20.00 GB", "1.00 MB/s", "12 / 40", "tsTorrentAction", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111"} {
		if !strings.Contains(body, want) {
			t.Errorf("the torrent cards lack %q", want)
		}
	}
}

func TestAnEmptyTorrServerSaysSo(t *testing.T) {
	h, session, _ := torrServers(t)
	if body := postTorrents(t, h, `{"torrents":[]}`, session).Body.String(); !strings.Contains(body, "No torrents") {
		t.Errorf("an empty list = %q, want a no-torrents note", body)
	}
}

func TestTorrentListsNeedASignedInUserAndAreBounded(t *testing.T) {
	h, session, _ := torrServers(t)
	if res := postTorrents(t, h, twoTorrents, nil); res.Code != http.StatusUnauthorized {
		t.Errorf("signed out = %d, want 401", res.Code)
	}
	huge := `{"torrents":[{"title":"` + strings.Repeat("x", 3<<20) + `"}]}`
	if res := postTorrents(t, h, huge, session); res.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("a 3 MB list = %d, want 413", res.Code)
	}
}

func TestTooManyTorrentsOrFilesAreRefused(t *testing.T) {
	h, session, _ := torrServers(t)
	many := `{"torrents":[` + strings.TrimSuffix(strings.Repeat(`{},`, 501), ",") + `]}`
	if res := postTorrents(t, h, many, session); res.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("501 torrents = %d, want 413", res.Code)
	}
	files := `{"torrents":[{"file_stats":[` + strings.TrimSuffix(strings.Repeat(`{},`, 5001), ",") + `]}]}`
	if res := postTorrents(t, h, files, session); res.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("a torrent of 5001 files = %d, want 413", res.Code)
	}
}
