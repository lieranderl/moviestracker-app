package handlers_test

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/engine"
)

// settingsEngine is a TorrServer whose settings the test controls and inspects.
type settingsEngine struct {
	*httptest.Server
	mu      sync.Mutex
	btsets  map[string]any
	gst     map[string]any // nil: built without GStreamer
	sets    []map[string]any
	gstSets []map[string]any
	gstDefs int
	hold    chan struct{} // when set, /stream blocks until closed
	toneMap bool          // GStreamer has the hdrtonemap element
}

func newSettingsEngine(t *testing.T, gst bool) *settingsEngine {
	t.Helper()
	e := &settingsEngine{btsets: map[string]any{
		"CacheSize": float64(64 << 20), "ReaderReadAHead": float64(95), "PreloadCache": float64(50),
		"ConnectionsLimit": float64(25), "DisableDHT": false, "EnableBonjour": true,
		"DefaultTrackers": "udp://tracker.example:1337", "FutureField": "kept",
	}}
	if gst {
		e.gst = map[string]any{"MaxTasks": float64(0), "SegmentSeconds": float64(6), "GSTPath": "/opt/gst", "TranscodeH265": false}
	}
	e.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/stream") && e.hold != nil {
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-e.hold
			return
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch r.URL.Path {
		case "/echo":
			_, _ = w.Write([]byte("MatriX.145"))
		case "/gst/echo":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"gstreamer":        map[string]any{"available": true, "works": true, "version": "1.28.7"},
				"hdr_tone_mapping": map[string]any{"available": e.toneMap, "works": e.toneMap},
			})
		case "/torrents":
			_, _ = w.Write([]byte(`[]`))
		case "/settings":
			if req["action"] == "set" {
				sets, _ := req["sets"].(map[string]any)
				e.sets = append(e.sets, sets)
				e.btsets = sets
				return
			}
			_ = json.NewEncoder(w).Encode(e.btsets)
		case "/gst/settings":
			switch {
			case e.gst == nil && r.Method == http.MethodGet:
				_, _ = w.Write([]byte(`{"built_in":false}`))
			case e.gst == nil:
				w.WriteHeader(http.StatusNotFound)
			case r.Method == http.MethodGet:
				_ = json.NewEncoder(w).Encode(map[string]any{"built_in": true, "config": e.gst, "defaults": map[string]any{}})
			case req["action"] == "def":
				e.gstDefs++
				_, _ = w.Write([]byte(`{}`))
			default:
				cfg, _ := req["config"].(map[string]any)
				e.gstSets = append(e.gstSets, cfg)
				e.gst = cfg
				_, _ = w.Write([]byte(`{}`))
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(func() {
		if e.hold != nil {
			close(e.hold)
		}
		e.Close()
	})
	return e
}

func (e *settingsEngine) lastSet() map[string]any {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.sets) == 0 {
		return nil
	}
	return e.sets[len(e.sets)-1]
}

func TestSavingASettingsSectionChangesOnlyItsFields(t *testing.T) {
	eng := newSettingsEngine(t, false)
	l := newLocal(t, withAdmin(t), withEngineAt(eng.URL))
	admin := l.admin(t)

	page := html.UnescapeString(l.do(t, httptest.NewRequest(http.MethodGet, "/settings/engine", nil), admin).Body.String())
	if !strings.Contains(page, "Connections per torrent") || !strings.Contains(page, `"ConnectionsLimit":25`) {
		t.Fatalf("engine settings page does not show the current values")
	}

	rr := l.action(t, "/api/settings/engine/engine", `{"engine":{"ConnectionsLimit":40,"DisableDHT":false,"DownloadRateLimit":0}}`, admin)
	got := eng.lastSet()
	if got == nil {
		t.Fatalf("no settings saved:\n%s", rr.Body.String())
	}
	// "DHT" is shown switched on (true); switching it off sends DisableDHT=true.
	if got["ConnectionsLimit"] != float64(40) || got["DisableDHT"] != true {
		t.Errorf("saved %v, want ConnectionsLimit 40 and DisableDHT true", got)
	}
	if got["FutureField"] != "kept" || got["CacheSize"] != float64(64<<20) || got["EnableBonjour"] != true {
		t.Errorf("fields outside the section changed: %v", got)
	}
	if !strings.Contains(rr.Body.String(), "Saved") {
		t.Errorf("no confirmation:\n%s", rr.Body.String())
	}
}

func TestUnitsAreConvertedAndOutOfRangeValuesRefused(t *testing.T) {
	eng := newSettingsEngine(t, false)
	l := newLocal(t, withAdmin(t), withEngineAt(eng.URL))
	admin := l.admin(t)

	rr := l.action(t, "/api/settings/engine/streaming", `{"streaming":{"CacheSize":2,"ReaderReadAHead":95,"PreloadCache":50}}`, admin)
	if eng.lastSet() != nil || !strings.Contains(rr.Body.String(), "RAM cache") {
		t.Fatalf("a 2 MB cache was accepted or not explained:\n%s", rr.Body.String())
	}
	l.action(t, "/api/settings/engine/streaming", `{"streaming":{"CacheSize":256,"ReaderReadAHead":90,"PreloadCache":20}}`, admin)
	if got := eng.lastSet(); got == nil || got["CacheSize"] != float64(256<<20) || got["ReaderReadAHead"] != float64(90) {
		t.Errorf("saved %v, want CacheSize 256 MB in bytes and read-ahead 90", got)
	}
}

func TestOnlyAnAdminOpensOrSavesSettings(t *testing.T) {
	eng := newSettingsEngine(t, false)
	l := newLocal(t, func(store *config.Store) {
		addAccount(t, store, "admin", config.RoleAdmin)
		addAccount(t, store, "kid", config.RoleViewer)
	}, withEngineAt(eng.URL))
	viewer := l.signIn(t, l.accounts.Lookup("kid"))
	if rr := l.do(t, httptest.NewRequest(http.MethodGet, "/settings/engine", nil), viewer); rr.Code != http.StatusForbidden {
		t.Errorf("viewer opening settings: %d, want 403", rr.Code)
	}
	if rr := l.action(t, "/api/settings/engine/engine", `{"engine":{"ConnectionsLimit":40}}`, viewer); rr.Code != http.StatusForbidden || eng.lastSet() != nil {
		t.Errorf("viewer saving settings: %d, want 403 and nothing saved", rr.Code)
	}
}

func TestSettingsWarnWhileSomethingIsPlaying(t *testing.T) {
	eng := newSettingsEngine(t, false)
	eng.hold = make(chan struct{})
	l := newLocal(t, withAdmin(t), withEngineAt(eng.URL))
	admin := l.admin(t)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	go l.do(t, httptest.NewRequest(http.MethodGet, "/api/torrserver/stream/stream/x.mkv?link="+duneHash+"&index=1&play", nil).WithContext(ctx), admin)
	time.Sleep(100 * time.Millisecond)

	page := l.do(t, httptest.NewRequest(http.MethodGet, "/settings/engine", nil), admin).Body.String()
	if !strings.Contains(page, "1 stream is playing") {
		t.Errorf("settings page does not warn about the stream playing")
	}
	stop()
	time.Sleep(100 * time.Millisecond)
	if page := l.do(t, httptest.NewRequest(http.MethodGet, "/settings/engine", nil), admin).Body.String(); strings.Contains(page, "is playing") {
		t.Errorf("warning still shown after the stream ended")
	}
}

func TestStartupOptionsNeedTheManagedEngine(t *testing.T) {
	eng := newSettingsEngine(t, false)
	l := newLocal(t, withAdmin(t), withEngineAt(eng.URL))
	admin := l.admin(t)
	page := l.do(t, httptest.NewRequest(http.MethodGet, "/settings/network", nil), admin).Body.String()
	if !strings.Contains(page, "Only when Moviestracker runs TorrServer") {
		t.Errorf("startup options of an external TorrServer are not marked read-only")
	}
	l.action(t, "/api/settings/engine/network", `{"network":{"PublicIPv4":"203.0.113.7"}}`, admin)
	if got := l.store.State().TorrServer.Startup.PublicIPv4; got != "" {
		t.Errorf("a startup option was saved for an external TorrServer: %q", got)
	}
}

func TestStartupOptionsRestartTheManagedEngine(t *testing.T) {
	opt, sup := withEngine(t)
	l := newLocal(t, withAdmin(t), opt)
	admin := l.admin(t)
	l.action(t, "/api/settings/sources/torrserver", `{"torrserverMode":"managed"}`, admin)
	before := sup.Status().PID

	rr := l.action(t, "/api/settings/engine/network", `{"network":{"PublicIPv4":"203.0.113.7","ProxyURL":"","ProxyMode":"tracker","PeersListenPort":0,"DisableUPNP":false,"EnableIPv6":false,"PublicIPv6":""}}`, admin)
	if got := l.store.State().TorrServer.Startup.PublicIPv4; got != "203.0.113.7" {
		t.Fatalf("public IPv4 not saved (%q):\n%s", got, rr.Body.String())
	}
	if st := sup.Status(); st.State != engine.Running || st.PID == before {
		t.Errorf("engine not restarted with the new option: %+v (was PID %d)", st, before)
	}
	if got := sup.Options().PublicIPv4; got != "203.0.113.7" {
		t.Errorf("engine options = %+v", sup.Options())
	}

	rr = l.action(t, "/api/settings/engine/network", `{"network":{"PublicIPv4":"not an ip"}}`, admin)
	if !strings.Contains(rr.Body.String(), "IPv4") || l.store.State().TorrServer.Startup.PublicIPv4 != "203.0.113.7" {
		t.Errorf("an invalid IPv4 was accepted or not explained:\n%s", rr.Body.String())
	}
}

func TestGStreamerSettingsAreChangedAndReset(t *testing.T) {
	eng := newSettingsEngine(t, true)
	l := newLocal(t, withAdmin(t), withEngineAt(eng.URL))
	admin := l.admin(t)

	page := l.do(t, httptest.NewRequest(http.MethodGet, "/settings/gstreamer", nil), admin).Body.String()
	if !strings.Contains(page, "Pipelines at once") {
		t.Fatalf("GStreamer settings page shows no settings")
	}
	l.action(t, "/api/settings/gstreamer", `{"gstreamer":{"MaxTasks":2,"SegmentSeconds":6,"TranscodeH265":true}}`, admin)
	eng.mu.Lock()
	sets := eng.gstSets
	eng.mu.Unlock()
	if len(sets) != 1 || sets[0]["MaxTasks"] != float64(2) || sets[0]["TranscodeH265"] != true || sets[0]["GSTPath"] != "/opt/gst" {
		t.Fatalf("GStreamer settings saved = %v", sets)
	}
	l.action(t, "/api/settings/gstreamer/reset", `{}`, admin)
	eng.mu.Lock()
	defer eng.mu.Unlock()
	if eng.gstDefs != 1 {
		t.Errorf("reset sent %d times, want 1", eng.gstDefs)
	}
}

func TestHDRToSDRCanOnlyBeTurnedOnWhenGStreamerCanToneMap(t *testing.T) {
	eng := newSettingsEngine(t, true)
	l := newLocal(t, withAdmin(t), withEngineAt(eng.URL))
	admin := l.admin(t)
	toggle := regexp.MustCompile(`<input id="setting-gstreamer-HDRToSDR"[^>]*>`)

	page := l.do(t, httptest.NewRequest(http.MethodGet, "/settings/gstreamer", nil), admin).Body.String()
	if tag := toggle.FindString(page); !strings.Contains(tag, "disabled") || !strings.Contains(page, "has no HDR tone mapper") {
		t.Errorf("without a tone mapper the toggle should be off limits and say why: %q", tag)
	}

	eng.mu.Lock()
	eng.toneMap = true
	eng.mu.Unlock()
	page = l.do(t, httptest.NewRequest(http.MethodGet, "/settings/gstreamer", nil), admin).Body.String()
	if tag := toggle.FindString(page); tag == "" || strings.Contains(tag, "disabled") {
		t.Errorf("with a tone mapper the toggle should work: %q", tag)
	}
}

func TestATorrServerWithoutGStreamerExplainsItself(t *testing.T) {
	eng := newSettingsEngine(t, false)
	l := newLocal(t, withAdmin(t), withEngineAt(eng.URL))
	page := l.do(t, httptest.NewRequest(http.MethodGet, "/settings/gstreamer", nil), l.admin(t)).Body.String()
	if !strings.Contains(page, "without GStreamer") || strings.Contains(page, "Pipelines at once") {
		t.Errorf("GStreamer page for a TorrServer without GStreamer:\n%s", page)
	}
}

func TestCancellingSharedLinksStopsTheOldOnes(t *testing.T) {
	eng := newFakeEngine(t, false)
	l := newLocal(t, withAdmin(t), withEngineAt(eng.URL))
	admin := l.admin(t)
	old := shareLink.FindString(l.do(t, httptest.NewRequest(http.MethodGet, "/torrserver", nil), admin).Body.String())
	if old == "" {
		t.Fatal("no share link on the page")
	}
	l.action(t, "/api/settings/security/cancel-links", `{}`, admin)
	if rr := l.do(t, httptest.NewRequest(http.MethodGet, old, nil), nil); rr.Code == http.StatusOK {
		t.Error("a cancelled link still streams")
	}
	fresh := shareLink.FindString(l.do(t, httptest.NewRequest(http.MethodGet, "/torrserver", nil), admin).Body.String())
	if rr := l.do(t, httptest.NewRequest(http.MethodGet, fresh, nil), nil); rr.Code != http.StatusOK {
		t.Errorf("a new link after cancelling: %d, want 200", rr.Code)
	}
}
