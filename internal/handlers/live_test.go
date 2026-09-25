package handlers_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/handlers"
)

// fastLive polls every 40ms so a test sees many rounds quickly.
func fastLive(c *handlers.Config) {
	c.LivePolling = handlers.LivePolling{Engine: 40 * time.Millisecond, Torrents: 40 * time.Millisecond, Player: 40 * time.Millisecond}
}

// openStreams holds the given SSE streams open together for d and returns their bodies.
func openStreams(t *testing.T, l *local, cookie *http.Cookie, d time.Duration, paths ...string) []string {
	t.Helper()
	bodies := make([]string, len(paths))
	var wg sync.WaitGroup
	for i, path := range paths {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), d)
			defer cancel()
			bodies[i] = l.do(t, httptest.NewRequest(http.MethodGet, path, nil).WithContext(ctx), cookie).Body.String()
		})
	}
	wg.Wait()
	return bodies
}

func TestManyOpenTorrentListsShareOneTorrServerPoll(t *testing.T) {
	engine := newFakeEngine(t, false)
	l := newLocal(t, withAdmin(t), withEngineAt(engine.URL), fastLive)
	stream := "/api/torrserver/torrents?stream=true"

	bodies := openStreams(t, l, l.admin(t), 400*time.Millisecond, stream, stream, stream, stream)
	for i, body := range bodies {
		if !strings.Contains(body, "Dune") {
			t.Errorf("stream %d never showed the torrent list:\n%s", i, body)
		}
	}
	// Four viewers for ~10 intervals: one shared poll is ~10 list calls; four
	// separate pollers would make ~40.
	if calls := len(engine.asked("/torrents")); calls > 16 {
		t.Errorf("%d TorrServer list calls for 4 open lists, want one shared poll (≤16)", calls)
	}
}

func TestTheDownloadCardAsksTorrServerNothingOfItsOwn(t *testing.T) {
	engine := newFakeEngine(t, false)
	l := newLocal(t, withAdmin(t), withEngineAt(engine.URL), fastLive)

	bodies := openStreams(t, l, l.admin(t), 400*time.Millisecond,
		"/api/torrserver/torrents?stream=true",
		"/api/torrserver/torrent-stats?hash="+duneHash+"&stream=true")
	if !strings.Contains(bodies[1], `id="torr-activity-stats"`) || !strings.Contains(bodies[1], "Working") {
		t.Errorf("download card stream shows no stats:\n%s", bodies[1])
	}
	if calls := len(engine.asked("/torrents")); calls > 16 {
		t.Errorf("%d list calls: the card polled on its own instead of sharing the list", calls)
	}
	if extra := len(engine.asked("/cache")); extra != 0 {
		t.Errorf("the card made %d /cache requests", extra)
	}
}

func TestThePlayerStreamKeepsTheGStreamerPipelineAliveItself(t *testing.T) {
	engine := newFakeEngine(t, true)
	l := newLocal(t, withAdmin(t), withEngineAt(engine.URL), fastLive)

	body := openStreams(t, l, l.admin(t), 400*time.Millisecond,
		"/api/torrserver/player-stats?stream=true&gst=1&hash="+duneHash+"&index=1&audio=0")[0]
	if !strings.Contains(body, `id="torr-player-stats"`) {
		t.Errorf("player stream shows no stats:\n%s", body)
	}
	if beats := len(engine.asked("/gst/" + duneHash + "/heartbeat")); beats < 3 {
		t.Errorf("%d GStreamer heartbeats in ~10 intervals, want the server to send them", beats)
	}
}

func TestClosingThePlayerEndsItsStream(t *testing.T) {
	engine := newFakeEngine(t, false)
	l := newLocal(t, withAdmin(t), withEngineAt(engine.URL), fastLive)
	rr := l.do(t, httptest.NewRequest(http.MethodGet, "/api/torrserver/player-stats?stop=1", nil), l.admin(t))
	if rr.Code != http.StatusNoContent {
		t.Errorf("stop request = %d, want an immediate 204", rr.Code)
	}
}

func TestReconnectingListsDoNotCallTorrServerThemselves(t *testing.T) {
	engine := newFakeEngine(t, false)
	slow := func(c *handlers.Config) {
		c.LivePolling = handlers.LivePolling{Engine: time.Hour, Torrents: time.Hour, Player: time.Hour}
	}
	l := newLocal(t, withAdmin(t), withEngineAt(engine.URL), slow)
	admin := l.admin(t)
	stream := "/api/torrserver/torrents?stream=true"

	// One page keeps the topics alive; they poll once, then not for an hour.
	holder, stop := context.WithCancel(context.Background())
	defer stop()
	go l.do(t, httptest.NewRequest(http.MethodGet, stream, nil).WithContext(holder), admin)
	time.Sleep(100 * time.Millisecond)
	before := len(engine.asked("/torrents")) + len(engine.asked("/echo"))

	// A hidden tab or a flaky network reconnects over and over.
	for range 5 {
		body := openStreams(t, l, admin, 50*time.Millisecond, stream)[0]
		if !strings.Contains(body, "Dune") {
			t.Fatalf("a reconnected stream shows no list:\n%s", body)
		}
	}
	if after := len(engine.asked("/torrents")) + len(engine.asked("/echo")); after != before {
		t.Errorf("5 reconnects made %d TorrServer calls, want 0: they must start from the shared snapshot", after-before)
	}
}
