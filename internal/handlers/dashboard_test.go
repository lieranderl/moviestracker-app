package handlers_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/events"
	"github.com/lieranderl/moviestracker-app/internal/handlers"
)

// playingEngine is a TorrServer with Dune downloading, whose /stream answers
// hold until the test lets them go.
func playingEngine(t *testing.T) (*httptest.Server, chan struct{}) {
	t.Helper()
	release := make(chan struct{})
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/echo":
			_, _ = w.Write([]byte("MatriX.145"))
		case r.URL.Path == "/torrents":
			_, _ = w.Write([]byte(`[{"hash":"` + duneHash + `","title":"Dune","stat":3,"download_speed":5242880,"upload_speed":1048576,` +
				`"connected_seeders":7,"active_peers":21,"loaded_size":1048576,"bytes_read_data":1073741824,"bytes_written_data":104857600,` +
				`"file_stats":[{"id":1,"path":"Dune (2021)/Dune.2021.mkv","length":10000000000}]},` +
				`{"hash":"` + strings.Repeat("b", 40) + `","title":"Heat","stat":5}]`))
		case strings.HasPrefix(r.URL.Path, "/stream/"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("video"))
			w.(http.Flusher).Flush()
			select {
			case <-release:
			case <-r.Context().Done():
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		engine.Close()
	})
	return engine, release
}

func quickDashboard(c *handlers.Config) {
	fastLive(c)
	c.StreamIdle = 150 * time.Millisecond
}

func TestTheDashboardShowsWhatIsPlayingAndWhereFrom(t *testing.T) {
	engine, release := playingEngine(t)
	l := newLocal(t, withAdmin(t), withEngineAt(engine.URL), quickDashboard)
	admin := l.admin(t)

	// Alex plays in the browser; a TV plays a shared link.
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go l.do(t, httptest.NewRequest(http.MethodGet, "/api/torrserver/stream/stream/Dune.2021.mkv?link="+duneHash+"&index=1&play", nil).WithContext(ctx), admin)
	link := shareLink.FindString(l.do(t, httptest.NewRequest(http.MethodGet, "/torrserver", nil), admin).Body.String())
	tv := httptest.NewRequest(http.MethodGet, link, nil).WithContext(ctx)
	tv.RemoteAddr = "192.168.1.31:50000"
	tv.Header.Set("Range", "bytes=2500000000-")
	go l.do(t, tv, nil)
	time.Sleep(150 * time.Millisecond)

	body := openStreams(t, l, admin, 300*time.Millisecond, "/api/dashboard?stream=true")[0]
	for _, want := range []string{
		`id="dash-streams"`, "Dune", "Dune.2021.mkv", "admin", "Shared link", "192.168.1.31", "25%", // the TV reads a quarter in
		`id="dash-swarm"`, "5.00 MB/s", "1.00 MB/s", "21", // totals of the list
		"Busiest now", "↓ 1.00 GB", "↑ 100 MB uploaded", // since TorrServer connected them
		`id="dash-pulse"`, "1 in the app · 1 shared link",
		"To the player", "Torrent download", // each stream's delivery against its torrent
		`id="dash-app"`, "MatriX.145",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard lacks %q", want)
		}
	}
	if strings.Contains(body, engine.URL) || strings.Contains(body, strings.TrimPrefix(engine.URL, "http://")) {
		t.Error("the dashboard contains the TorrServer address")
	}

	close(release)
	stop()
	time.Sleep(300 * time.Millisecond) // past the idle time
	body = openStreams(t, l, admin, 200*time.Millisecond, "/api/dashboard?stream=true")[0]
	if !strings.Contains(body, "Nothing is playing") {
		t.Errorf("finished streams still listed:\n%s", body)
	}
	played := body[strings.Index(body, `id="dash-played"`):]
	for _, want := range []string{"Dune", "admin", "Shared link", "192.168.1.31"} {
		if !strings.Contains(played, want) {
			t.Errorf("Played today lacks %q", want)
		}
	}
}

func TestTheDashboardNeedsSigningIn(t *testing.T) {
	engine, _ := playingEngine(t)
	l := newLocal(t, withAdmin(t), withEngineAt(engine.URL))
	if rr := l.do(t, httptest.NewRequest(http.MethodGet, "/dashboard", nil), nil); rr.Code != http.StatusSeeOther {
		t.Errorf("signed-out dashboard page: %d, want a redirect to sign in", rr.Code)
	}
	if rr := l.do(t, httptest.NewRequest(http.MethodGet, "/api/dashboard?stream=true", nil), nil); rr.Code != http.StatusUnauthorized {
		t.Errorf("signed-out dashboard stream: %d, want 401", rr.Code)
	}
}

func TestTheDashboardShowsWhatGStreamerIsDoing(t *testing.T) {
	engine := newFakeEngine(t, true)
	l := newLocal(t, withAdmin(t), withEngineAt(engine.URL), quickDashboard)
	admin := l.admin(t)
	// The browser player asks GStreamer for Dune as HLS.
	l.do(t, httptest.NewRequest(http.MethodGet, "/api/torrserver/stream/gst/"+duneHash+"/master.m3u8?index=1", nil), admin)
	l.do(t, httptest.NewRequest(http.MethodGet, "/api/torrserver/stream/gst/"+duneHash+"/seg/0.m4s?audio=0&index=1", nil), admin)

	body := openStreams(t, l, admin, 300*time.Millisecond, "/api/dashboard?stream=true")[0]
	for _, want := range []string{
		"GStreamer", "1.28.7", "1 HLS stream", "H.264", "part of TorrServer",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard lacks %q", want)
		}
	}
}

func TestWithoutGStreamerTheDashboardSaysWhereMKVPlays(t *testing.T) {
	engine := newFakeEngine(t, false)
	l := newLocal(t, withAdmin(t), withEngineAt(engine.URL), quickDashboard)
	body := openStreams(t, l, l.admin(t), 300*time.Millisecond, "/api/dashboard?stream=true")[0]
	if !strings.Contains(body, "not installed") {
		t.Error("the System card does not say GStreamer is missing")
	}
}

// section is the latest patch of the card with id, up to the next card.
func section(t *testing.T, body, id string) string {
	t.Helper()
	start := strings.LastIndex(body, `id="`+id+`"`) // the latest patch
	if start < 0 {
		t.Fatalf("no %s in:\n%s", id, body)
	}
	rest := body[start+1:]
	if end := strings.Index(rest, `id="dash-`); end >= 0 {
		rest = rest[:end]
	}
	return rest
}

func TestTheDashboardKeepsTheAppApartFromTheMachine(t *testing.T) {
	engine := newFakeEngine(t, true) // an external TorrServer on this machine
	l := newLocal(t, withAdmin(t), withEngineAt(engine.URL), quickDashboard)
	body := openStreams(t, l, l.admin(t), 400*time.Millisecond, "/api/dashboard?stream=true")[0]

	app := section(t, body, "dash-app")
	for _, want := range []string{
		"Moviestracker", "TorrServer", " MB", // found by its port, measured like a managed one
		"GStreamer", "part of TorrServer",
		"Together", "RAM cache",
		"Goroutines", "Heap", "go1.", // the Go runtime
		"Live streams", "Patches", // Datastar over SSE: this dashboard is one
	} {
		if !strings.Contains(app, want) {
			t.Errorf("app card lacks %q", want)
		}
	}
	machine := section(t, body, "dash-system")
	for _, want := range []string{"This machine", "CPU", " cores", "Load", "Memory", " of ", "free of"} {
		if !strings.Contains(machine, want) {
			t.Errorf("machine card lacks %q", want)
		}
	}
	for _, app := range []string{"TorrServer", "Moviestracker", "RAM cache"} {
		if strings.Contains(machine, app) {
			t.Errorf("machine card mixes in %q", app)
		}
	}
	if pulse := section(t, body, "dash-pulse"); strings.Contains(pulse, "CPU") || strings.Contains(pulse, "Memory") {
		t.Error("the app's headline figures mix in the machine's CPU and memory")
	}
}

func TestATorrServerOnAnotherMachineIsNamedNotMeasured(t *testing.T) {
	l := newLocal(t, withAdmin(t), withEngineAt("http://192.0.2.10:8090"), quickDashboard)
	body := openStreams(t, l, l.admin(t), 400*time.Millisecond, "/api/dashboard?stream=true")[0]
	if !strings.Contains(body, "runs on another machine") {
		t.Error("the System card does not say where a remote TorrServer runs")
	}
}

func TestThePeopleCardShowsWhoIsOnlineAndOnWhichDevice(t *testing.T) {
	engine := newFakeEngine(t, false)
	l := newLocal(t, func(st *config.Store) {
		addAccount(t, st, "admin", config.RoleAdmin)
		addAccount(t, st, "anna", config.RoleViewer)
	}, withEngineAt(engine.URL), quickDashboard)
	phone := httptest.NewRequest(http.MethodGet, "/search", nil)
	phone.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 19_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/19.0 Mobile/15E148 Safari/604.1")
	phone.RemoteAddr = "192.168.1.44:50000"
	l.do(t, phone, l.signIn(t, l.accounts.Lookup("anna")))

	body := openStreams(t, l, l.admin(t), 400*time.Millisecond, "/api/dashboard?stream=true")[0]
	card := body[strings.Index(body, `id="dash-people"`):]
	for _, want := range []string{"anna", "Safari on iPhone", "192.168.1.44", "admin"} {
		if !strings.Contains(card, want) {
			t.Errorf("People card lacks %q", want)
		}
	}
}

func TestTheProblemsCardShowsRecentWarningsAndErrors(t *testing.T) {
	engine := newFakeEngine(t, false)
	rec := events.NewRecorder(slog.NewTextHandler(io.Discard, nil), 10)
	l := newLocal(t, withAdmin(t), withEngineAt(engine.URL), quickDashboard, func(c *handlers.Config) { c.Events = rec })
	quiet := openStreams(t, l, l.admin(t), 300*time.Millisecond, "/api/dashboard?stream=true")[0]
	if !strings.Contains(quiet, "No problems since Moviestracker started") {
		t.Error("with nothing logged, the Problems card is not calm")
	}
	slog.New(rec).Error("TorrServer did not start", "error", errors.New("port 18090 in use"))
	body := openStreams(t, l, l.admin(t), 300*time.Millisecond, "/api/dashboard?stream=true")[0]
	card := body[strings.Index(body, `id="dash-problems"`):]
	for _, want := range []string{"TorrServer did not start", "port 18090 in use"} {
		if !strings.Contains(card, want) {
			t.Errorf("Problems card lacks %q", want)
		}
	}
}
