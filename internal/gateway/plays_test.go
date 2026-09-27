package gateway_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/streams"
)

// play asks for bytes 100–199 of a stream, as a video player seeking does.
func (s *setup) play(t *testing.T, target, user, password, remote string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.RemoteAddr = remote
	req.Header.Set("Range", "bytes=100-199")
	if user != "" {
		req.SetBasicAuth(user, password)
	}
	rec := httptest.NewRecorder()
	s.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusPartialContent {
		t.Fatalf("GET %s = %d, want the stream", target, rec.Code)
	}
}

func (s *setup) playing(t *testing.T) streams.Session {
	t.Helper()
	active := s.plays.Active()
	if len(active) != 1 {
		t.Fatalf("the dashboard shows %d streams, want 1: %+v", len(active), active)
	}
	return active[0]
}

func TestTheDashboardShowsWhatAnAppPlaysAndWho(t *testing.T) {
	s := newSetup(t)
	s.engine.saved[savedHash] = true
	user, password := s.addLogin(t, "Lampa")

	s.play(t, "/stream/Movie.mkv?link="+savedHash+"&index=2&play", user, password, tv)

	got := s.playing(t)
	if got.Viewer != "Lampa" || !got.OtherApp || got.Client != "192.168.1.40" || got.Hash != savedHash ||
		got.File != 2 || got.Kind != streams.Direct || got.Offset != 100 || got.Bytes != 100 {
		t.Errorf("the dashboard shows %+v, want Lampa on 192.168.1.40 playing file 2, 100 bytes sent from byte 100", got)
	}
}

// A TV app signs in, then hands the stream's link to a video player, which
// cannot: the stream is the app's that signed in from that address.
func TestAStreamWithoutALoginIsTheAppsThatSignedInFromTheSameDevice(t *testing.T) {
	s := newSetup(t)
	s.engine.saved[savedHash] = true
	user, password := s.addLogin(t, "Living room TV")
	if rec := s.ask(t, http.MethodPost, "/torrents", `{"action":"list"}`, user, password, tv); rec.Code != http.StatusOK {
		t.Fatalf("list = %d", rec.Code)
	}

	s.play(t, "/play/"+savedHash+"/1", "", "", "192.168.1.40:52000")

	if got := s.playing(t); got.Viewer != "Living room TV" || !got.OtherApp || got.File != 1 {
		t.Errorf("the dashboard shows %+v, want the Living room TV playing file 1", got)
	}
}

func TestAStreamFromADeviceNoAppSignedInFromIsAnOtherApp(t *testing.T) {
	s := newSetup(t)
	s.engine.saved[savedHash] = true
	user, password := s.addLogin(t, "Living room TV")
	s.ask(t, http.MethodPost, "/torrents", `{"action":"list"}`, user, password, tv)

	s.play(t, "/stream?link="+savedHash+"&index=1&play", "", "", "192.168.1.41:52000")

	if got := s.playing(t); got.Viewer != "" || !got.OtherApp || got.Client != "192.168.1.41" {
		t.Errorf("the dashboard shows %+v, want an unnamed other app on 192.168.1.41", got)
	}
}

func TestOnlyPlaybackShowsOnTheDashboard(t *testing.T) {
	s := newSetup(t)
	s.engine.saved[savedHash] = true
	user, password := s.addLogin(t, "Lampa")

	for _, target := range []string{
		"/torrents",
		"/stream?link=" + savedHash + "&index=1&stat",
		"/stream?link=" + savedHash + "&index=1&m3u",
		"/playlist?hash=" + savedHash,
	} {
		s.ask(t, http.MethodGet, target, "", user, password, tv)
	}
	// Refused: not counted either.
	s.ask(t, http.MethodGet, "/stream?link=fedcba9876543210fedcba9876543210fedcba98&index=1&play", "", "", "", tv)

	if got := s.plays.Active(); len(got) != 0 {
		t.Errorf("the dashboard shows %+v, want nothing playing", got)
	}
}

func TestTheDashboardShowsAnAppsHLSStream(t *testing.T) {
	s := newSetup(t)
	user, password := s.addLogin(t, "Lampa")

	s.ask(t, http.MethodGet, "/gst/"+savedHash+"/master.m3u8?index=3&audio=1", "", user, password, tv)
	s.ask(t, http.MethodGet, "/gst/"+savedHash+"/seg/42.m4s", "", user, password, tv)

	if got := s.playing(t); got.Viewer != "Lampa" || got.Kind != streams.HLS || got.File != 3 || got.Audio != 1 || got.Segment != 42 {
		t.Errorf("the dashboard shows %+v, want Lampa playing file 3 as HLS at segment 42", got)
	}
}
