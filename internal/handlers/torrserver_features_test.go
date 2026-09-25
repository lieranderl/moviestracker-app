package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/auth"
)

// fakeTorrServer answers the TorrServer endpoints the page relies on. gst
// toggles whether /gst/echo reports a working GStreamer.
func fakeTorrServer(gst bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/echo":
			_, _ = w.Write([]byte("MatriX.145"))
		case "/gst/echo":
			if !gst {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`{"gstreamer":{"available":true,"works":true,"version":"1.28.7"}}`))
		case "/torrents":
			// Like upstream, "list" carries the active torrent's live status.
			hash1 := `{"hash":"hash1","title":"Movie 1","file_stats":[{"id":1,"path":"season1/ep1.mkv","length":1000}],` +
				`"stat":3,"stat_string":"Torrent working","active_peers":7,"total_peers":30,"connected_seeders":5,` +
				`"download_speed":558359.06,"preloaded_bytes":33554432}`
			if strings.Contains(readBody(r), `"list"`) {
				_, _ = w.Write([]byte("[" + hash1 + "]"))
				return
			}
			_, _ = w.Write([]byte(hash1))
		case "/settings":
			_, _ = w.Write([]byte(`{"CacheSize":67108864,"PreloadCache":50}`))
		case "/gst/hash1/probe":
			_, _ = w.Write([]byte(`{"Container":"Matroska","DurationNS":7133792000000,"FileSize":22915459964,"Tracks":[` +
				`{"Index":0,"Type":"video","CapsName":"video/x-h265","Codec":"video/x-h265, profile=(string)main-10, width=(int)3840",` +
				`"Width":3840,"Height":1606,"FrameRateNum":24,"FrameRateDen":1,"BitDepth":10,"VideoTransfer":"pq","HasMasteringDisplayInfo":true},` +
				`{"Index":0,"Type":"audio","CapsName":"audio/x-ac3","Title":"Dub","Language":"ru","Channels":6,"Rate":48000},` +
				`{"Index":1,"Type":"audio","CapsName":"audio/x-eac3","Title":"Original","Language":"en","Channels":6,"Rate":48000},` +
				`{"Index":0,"Type":"subtitle","CapsName":"text/x-raw","Title":"Full","Language":"en"}]}`))
		case "/gst/hash1/master.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n" +
				`#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",NAME="Full",LANGUAGE="en",URI="/gst/hash1/subs/0.m3u8"` + "\n" +
				`#EXT-X-STREAM-INF:BANDWIDTH=38546893,AVERAGE-BANDWIDTH=25697929,RESOLUTION=3840x1606,FRAME-RATE=24,CODECS="hvc1.2.4.H150.B0,mp4a.40.2",VIDEO-RANGE=PQ,SUBTITLES="subs"` + "\n" +
				"/gst/hash1/video.m3u8?audio=1\n"))
		default:
			http.NotFound(w, r)
		}
	})
}

func readBody(r *http.Request) string {
	var sb strings.Builder
	buf := make([]byte, 512)
	for {
		n, err := r.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			return sb.String()
		}
	}
}

func signedIn(t *testing.T, sessions *auth.SessionManager, req *http.Request) *http.Request {
	t.Helper()
	id, err := sessions.CreateSession(&auth.User{Username: "u"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	req.AddCookie(sessionCookie(id))
	return req
}

func TestGStreamerSupportIsPushedAsSignal(t *testing.T) {
	for _, tc := range []struct {
		gst  bool
		want string
	}{{true, `"gst":true`}, {false, `"gst":false`}} {
		srv, sessions, ts := setupTestServerWithTorrServer(t, fakeTorrServer(tc.gst))
		rr := httptest.NewRecorder()
		srv.ServeHTTP(rr, signedIn(t, sessions, httptest.NewRequest(http.MethodGet, "/api/torrserver/torrents", nil)))
		ts.Close()
		if !strings.Contains(rr.Body.String(), tc.want) {
			t.Errorf("gst=%v: expected %s in stream, got:\n%s", tc.gst, tc.want, rr.Body.String())
		}
	}
}

func tracksRequest(target, audioLang string) *http.Request {
	signals := url.QueryEscape(`{"audioLang":"` + audioLang + `"}`)
	req := httptest.NewRequest(http.MethodGet, target+"&datastar="+signals, nil)
	req.Header.Set("Datastar-Request", "true")
	return req
}

func TestPlayerPicksAudioTrackInRememberedLanguage(t *testing.T) {
	for _, tc := range []struct {
		name, target, lang, want string
	}{
		{"preferred language present", "/api/torrserver/tracks?hash=hash1&index=1", "en", `"audioTrack":1`},
		{"no preference falls back to first track", "/api/torrserver/tracks?hash=hash1&index=1", "", `"audioTrack":0`},
		{"unknown language falls back to first track", "/api/torrserver/tracks?hash=hash1&index=1", "de", `"audioTrack":0`},
		{"probe failure still starts playback", "/api/torrserver/tracks?hash=missing&index=1", "en", `"audioTrack":0`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, sessions, ts := setupTestServerWithTorrServer(t, fakeTorrServer(true))
			defer ts.Close()
			rr := httptest.NewRecorder()
			srv.ServeHTTP(rr, signedIn(t, sessions, tracksRequest(tc.target, tc.lang)))
			if !strings.Contains(rr.Body.String(), tc.want) {
				t.Fatalf("expected %s, got:\n%s", tc.want, rr.Body.String())
			}
		})
	}
}

func TestPlayerListsAudioTracksWithLanguageAndTitle(t *testing.T) {
	srv, sessions, ts := setupTestServerWithTorrServer(t, fakeTorrServer(true))
	defer ts.Close()
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, signedIn(t, sessions, tracksRequest("/api/torrserver/tracks?hash=hash1&index=1", "")))
	body := rr.Body.String()
	for _, want := range []string{
		`id="torr-audio-picker"`,
		`data-track="0" data-lang="ru"`,
		`>RU · Dub · 5.1<`,
		`data-track="1" data-lang="en"`,
		`>EN · Original · 5.1<`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %s in:\n%s", want, body)
		}
	}
}

func TestPlayerShowsLiveTorrentStatsUnderVideo(t *testing.T) {
	srv, sessions, ts := setupTestServerWithTorrServer(t, fakeTorrServer(true))
	defer ts.Close()
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, signedIn(t, sessions, httptest.NewRequest(http.MethodGet, "/api/torrserver/player-stats?hash=hash1", nil)))
	body := rr.Body.String()
	for _, want := range []string{
		`id="torr-player-stats"`,
		`Peers 7/30`,
		`Seeds 5`,
		`545.27 KB/s`,
		`Buffer 50%`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %q in:\n%s", want, body)
		}
	}
}

func TestPlayerStatsKeepGStreamerPipelineAlive(t *testing.T) {
	var heartbeats int
	fake := fakeTorrServer(true)
	srv, sessions, ts := setupTestServerWithTorrServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/gst/hash1/heartbeat" {
			heartbeats++
			return
		}
		fake.ServeHTTP(w, r)
	}))
	defer ts.Close()
	for _, target := range []string{"/api/torrserver/player-stats?hash=hash1", "/api/torrserver/player-stats?hash=hash1&gst=1"} {
		srv.ServeHTTP(httptest.NewRecorder(), signedIn(t, sessions, httptest.NewRequest(http.MethodGet, target, nil)))
	}
	if heartbeats != 1 {
		t.Fatalf("expected 1 GStreamer heartbeat (only for gst=1), got %d", heartbeats)
	}
}

func TestPlayerStatsShowUnavailableWhenTorrServerFails(t *testing.T) {
	srv, sessions, ts := setupTestServerWithTorrServer(t, http.NotFoundHandler())
	defer ts.Close()
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, signedIn(t, sessions, httptest.NewRequest(http.MethodGet, "/api/torrserver/player-stats?hash=hash1", nil)))
	if !strings.Contains(rr.Body.String(), "Stats unavailable") {
		t.Fatalf("expected unavailable badge, got:\n%s", rr.Body.String())
	}
}

func TestMediaInfoComparesSourceWithHLSOutput(t *testing.T) {
	srv, sessions, ts := setupTestServerWithTorrServer(t, fakeTorrServer(true))
	defer ts.Close()
	info := "/api/torrserver/player-stats?hash=hash1&info=1&index=1&audio=1"

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, signedIn(t, sessions, httptest.NewRequest(http.MethodGet, info, nil)))
	for _, want := range []string{`id="torr-media-info"`, "Matroska", "HEVC Main 10", "3840×1606", "24 fps", "10-bit", "HDR10",
		"E-AC3 5.1 · 48 kHz", "21.34 GB", "01:58:53", "25.7 Mbps", "Waiting for stream"} {
		if !strings.Contains(rr.Body.String(), want) {
			t.Errorf("before playback: expected %q in:\n%s", want, rr.Body.String())
		}
	}

	// The player loads the master playlist through the proxy; its variant describes the output.
	srv.ServeHTTP(httptest.NewRecorder(), signedIn(t, sessions,
		httptest.NewRequest(http.MethodGet, "/api/torrserver/stream/gst/hash1/master.m3u8?index=1&audio=1", nil)))

	rr = httptest.NewRecorder()
	srv.ServeHTTP(rr, signedIn(t, sessions, httptest.NewRequest(http.MethodGet, info, nil)))
	body := rr.Body.String()
	for _, want := range []string{"HEVC · Remux", "AAC · Transcoded", "HDR10 · Kept", "38.5 Mbps peak", "1 WebVTT"} {
		if !strings.Contains(body, want) {
			t.Errorf("after playback: expected %q in:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Waiting for stream") {
		t.Errorf("output should be known once the master playlist was served")
	}
}

func TestMediaInfoIsSkippedWhilePanelIsClosed(t *testing.T) {
	srv, sessions, ts := setupTestServerWithTorrServer(t, fakeTorrServer(true))
	defer ts.Close()
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, signedIn(t, sessions, httptest.NewRequest(http.MethodGet, "/api/torrserver/player-stats?hash=hash1", nil)))
	if strings.Contains(rr.Body.String(), "torr-media-info") {
		t.Fatalf("media info should only be sent when requested")
	}
}

func TestPlayerOffersSubtitleMenuWithOffAndEachTrack(t *testing.T) {
	srv, sessions, ts := setupTestServerWithTorrServer(t, fakeTorrServer(true))
	defer ts.Close()
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, signedIn(t, sessions, tracksRequest("/api/torrserver/tracks?hash=hash1&index=1", "")))
	body := rr.Body.String()
	for _, want := range []string{
		`id="torr-subtitle-menu"`,
		`data-track="-1"`,
		`>Off<`,
		`data-track="0"`,
		`>EN · Full<`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %s in:\n%s", want, body)
		}
	}
}

func TestHLSOutputIsRememberedPerNumericTrack(t *testing.T) {
	srv, sessions, ts := setupTestServerWithTorrServer(t, fakeTorrServer(true))
	defer ts.Close()

	// Junk track values are ignored, and "01" is the same track as "1".
	for _, q := range []string{"index=x&audio=1", "index=1&audio=-3", "index=01&audio=01"} {
		srv.ServeHTTP(httptest.NewRecorder(), signedIn(t, sessions,
			httptest.NewRequest(http.MethodGet, "/api/torrserver/stream/gst/hash1/master.m3u8?"+q, nil)))
	}

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, signedIn(t, sessions, httptest.NewRequest(http.MethodGet, "/api/torrserver/player-stats?hash=hash1&info=1&index=1&audio=1", nil)))
	if !strings.Contains(rr.Body.String(), "HEVC · Remux") {
		t.Fatalf("output played as index=01&audio=01 not found for track 1/1:\n%s", rr.Body.String())
	}
}
