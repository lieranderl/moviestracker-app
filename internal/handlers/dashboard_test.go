package handlers_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/events"
	"github.com/lieranderl/moviestracker-app/internal/handlers"
	"github.com/lieranderl/moviestracker-app/internal/streams"
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
				`"connected_seeders":7,"active_peers":21,"total_peers":40,"torrent_size":10000000000,"loaded_size":1048576,"bytes_read_data":1073741824,"bytes_written_data":104857600,` +
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
		"↓ 1.00 GB", "↑ 100 MB uploaded", // since TorrServer connected them
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

func TestTheDashboardShowsWhatOtherAppsPlayAndEachActiveTorrent(t *testing.T) {
	engine, _ := playingEngine(t)
	plays := streams.New(time.Minute)
	l := newLocal(t, withAdmin(t), withEngineAt(engine.URL), fastLive, func(c *handlers.Config) { c.Plays = plays })
	admin := l.admin(t)
	// Lampa on the TV plays Dune through the port for other apps, halfway in.
	plays.Observe(streams.Request{Client: "192.168.1.40", Viewer: "Lampa", OtherApp: true, Hash: duneHash, File: 1, Kind: streams.Direct, Segment: -1, Offset: 5_000_000_000, Bytes: 1 << 20})

	body := openStreams(t, l, admin, 300*time.Millisecond, "/api/dashboard?stream=true")[0]
	// card is the latest patch of a card: its SSE event.
	card := func(id string) string {
		start := strings.LastIndex(body, `id="`+id+`"`)
		if start < 0 {
			t.Fatalf("no %s card", id)
		}
		end := strings.Index(body[start:], "\n\n")
		if end < 0 {
			end = len(body) - start
		}
		return body[start : start+end]
	}
	for _, c := range []struct {
		id   string
		want []string
	}{
		{"dash-pulse", []string{"1 in another app"}},
		{"dash-streams", []string{"Dune", "Dune.2021.mkv", "Lampa", "Other app", "192.168.1.40", "50%"}},
		{"dash-swarm", []string{
			"Dune", "Working", "9.31 GB", // title, status, size
			"5.00 MB/s", "1.00 MB/s", "21 of 40 · 7 seeders", "↓ 1.00 GB", "↑ 100 MB",
			"Dune.2021.mkv", "Lampa", // what of it plays, and who
		}},
	} {
		got := card(c.id)
		for _, want := range c.want {
			if !strings.Contains(got, want) {
				t.Errorf("%s lacks %q", c.id, want)
			}
		}
	}
	if swarm := card("dash-swarm"); strings.Contains(swarm, "Heat") {
		t.Error("the torrents card lists Heat, which is not active")
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
	wants := []string{"This machine", "CPU", " cores", "Memory", " of ", "free of"}
	if runtime.GOOS != "windows" { // Windows has no load averages, only an estimate that starts at zero
		wants = append(wants, "Load")
	}
	for _, want := range wants {
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

// httpsEngine is a MatriX.146 TorrServer serving HTTPS on 8091 with a Let's
// Encrypt certificate for ts.example valid until notAfter, or one it cannot load.
func httpsEngine(t *testing.T, notAfter time.Time, loadError string) *settingsEngine {
	t.Helper()
	eng := newSettingsEngine(t, false)
	eng.ssl = `{"enabled":true,"port":"8091","http_port":"8090","http_enabled":true,"cert":{"source":"user",
"cert_file":"/etc/letsencrypt/live/ts.example/fullchain.pem","key_file":"/etc/letsencrypt/live/ts.example/privkey.pem",
"issuer":"CN=R11,O=Let's Encrypt,C=US","dns_names":["ts.example"],"trusted":true,
"not_before":"2026-01-01T00:00:00Z","not_after":"` + notAfter.UTC().Format(time.RFC3339) + `","error":"` + loadError + `"}}`
	return eng
}

func TestTheDashboardShowsTorrServersHTTPSCertificate(t *testing.T) {
	eng := httpsEngine(t, time.Now().AddDate(0, 2, 0), "")
	l := newLocal(t, withAdmin(t), withEngineAt(eng.URL), quickDashboard)
	body := openStreams(t, l, l.admin(t), 400*time.Millisecond, "/api/dashboard?stream=true")[0]

	app := section(t, body, "dash-app")
	for _, want := range []string{"HTTPS", ":8091", "ts.example", "Let&#39;s Encrypt (R11)", "trusted", time.Now().AddDate(0, 2, 0).UTC().Format(time.DateOnly)} {
		if !strings.Contains(app, want) {
			t.Errorf("app card lacks %q", want)
		}
	}
	if problems := section(t, body, "dash-problems"); !strings.Contains(problems, "No problems") {
		t.Errorf("a certificate valid for two months is flagged:\n%s", problems)
	}
}

func TestTheDashboardWarnsTwoWeeksBeforeTheHTTPSCertificateExpires(t *testing.T) {
	eng := httpsEngine(t, time.Now().Add(5*24*time.Hour+time.Hour), "")
	l := newLocal(t, withAdmin(t), withEngineAt(eng.URL), quickDashboard)
	body := openStreams(t, l, l.admin(t), 400*time.Millisecond, "/api/dashboard?stream=true")[0]
	if problems := section(t, body, "dash-problems"); !strings.Contains(problems, "HTTPS certificate expires in 5 days") {
		t.Errorf("Problems card does not warn about the certificate:\n%s", problems)
	}
}

func TestTheDashboardSaysWhenTorrServerCannotLoadItsCertificate(t *testing.T) {
	eng := httpsEngine(t, time.Now().AddDate(1, 0, 0), "open /etc/letsencrypt/live/ts.example/privkey.pem: permission denied")
	l := newLocal(t, withAdmin(t), withEngineAt(eng.URL), quickDashboard)
	body := openStreams(t, l, l.admin(t), 400*time.Millisecond, "/api/dashboard?stream=true")[0]
	if problems := section(t, body, "dash-problems"); !strings.Contains(problems, "cannot load its HTTPS certificate") || !strings.Contains(problems, "permission denied") {
		t.Errorf("Problems card does not say the certificate fails to load:\n%s", problems)
	}
}
