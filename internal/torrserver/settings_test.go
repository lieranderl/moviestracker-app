package torrserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

// settingsEngine is a TorrServer whose settings requests the test inspects.
type settingsEngine struct {
	mu       sync.Mutex
	btsets   map[string]any
	gst      map[string]any
	gstBuilt bool
	sets     []map[string]any // bodies of "set" calls
	gstSets  []map[string]any
}

func (e *settingsEngine) serve(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		defer e.mu.Unlock()
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		switch r.URL.Path {
		case "/settings":
			switch req["action"] {
			case "get":
				_ = json.NewEncoder(w).Encode(e.btsets)
			case "set":
				sets, _ := req["sets"].(map[string]any)
				e.sets = append(e.sets, sets)
				e.btsets = sets
			default:
				w.WriteHeader(http.StatusBadRequest)
			}
		case "/gst/settings":
			if r.Method == http.MethodGet {
				if !e.gstBuilt {
					_, _ = w.Write([]byte(`{"built_in":false}`))
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"built_in": true, "config": e.gst, "defaults": map[string]any{"SegmentSeconds": 6}})
				return
			}
			if !e.gstBuilt {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			switch req["action"] {
			case "def":
				e.gst = map[string]any{"SegmentSeconds": float64(6)}
			default:
				cfg, _ := req["config"].(map[string]any)
				e.gstSets = append(e.gstSets, cfg)
				e.gst = cfg
			}
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestSavingSettingsKeepsEveryFieldItDoesNotChange(t *testing.T) {
	engine := &settingsEngine{btsets: map[string]any{
		"CacheSize": float64(64 << 20), "ConnectionsLimit": float64(25), "DisableDHT": false,
		"DefaultTrackers": "udp://tracker.example:1337", "FutureField": "from a newer TorrServer",
	}}
	c := torrserver.NewClient(engine.serve(t).URL, nil)
	ctx := context.Background()

	sets, err := c.Settings(ctx)
	if err != nil || sets.Int("ConnectionsLimit") != 25 || sets.Bool("DisableDHT") || sets.String("DefaultTrackers") != "udp://tracker.example:1337" {
		t.Fatalf("Settings() = %v, %v", sets, err)
	}
	if err := c.UpdateSettings(ctx, map[string]any{"ConnectionsLimit": 50, "DisableDHT": true}); err != nil {
		t.Fatalf("UpdateSettings(): %v", err)
	}
	if len(engine.sets) != 1 {
		t.Fatalf("%d set calls, want 1", len(engine.sets))
	}
	got := engine.sets[0]
	if got["ConnectionsLimit"] != float64(50) || got["DisableDHT"] != true {
		t.Errorf("changed fields not sent: %v", got)
	}
	if got["FutureField"] != "from a newer TorrServer" || got["DefaultTrackers"] != "udp://tracker.example:1337" || got["CacheSize"] != float64(64<<20) {
		t.Errorf("fields the update did not touch were lost: %v", got)
	}
}

func TestCacheSizeIsReadAfreshAfterSettingsChange(t *testing.T) {
	engine := &settingsEngine{btsets: map[string]any{"CacheSize": float64(64 << 20)}}
	c := torrserver.NewClient(engine.serve(t).URL, nil)
	ctx := context.Background()
	if n, _ := c.CacheSize(ctx); n != 64<<20 {
		t.Fatalf("CacheSize() = %d", n)
	}
	if err := c.UpdateSettings(ctx, map[string]any{"CacheSize": 256 << 20}); err != nil {
		t.Fatal(err)
	}
	if n, _ := c.CacheSize(ctx); n != 256<<20 {
		t.Errorf("CacheSize() after changing it = %d, want 256 MB", n)
	}
}

func TestGStreamerSettingsCanBeReadChangedAndReset(t *testing.T) {
	engine := &settingsEngine{gstBuilt: true, gst: map[string]any{"SegmentSeconds": float64(6), "MaxTasks": float64(2), "GSTPath": "/opt/gst"}}
	c := torrserver.NewClient(engine.serve(t).URL, nil)
	ctx := context.Background()

	gst, err := c.GSTSettings(ctx)
	if err != nil || !gst.BuiltIn || gst.Config.Int("MaxTasks") != 2 || gst.Defaults.Int("SegmentSeconds") != 6 {
		t.Fatalf("GSTSettings() = %+v, %v", gst, err)
	}
	if err := c.UpdateGSTSettings(ctx, map[string]any{"MaxTasks": 4}); err != nil {
		t.Fatalf("UpdateGSTSettings(): %v", err)
	}
	if got := engine.gstSets[0]; got["MaxTasks"] != float64(4) || got["GSTPath"] != "/opt/gst" {
		t.Errorf("GStreamer config sent = %v, want MaxTasks changed and GSTPath kept", got)
	}
	if err := c.ResetGSTSettings(ctx); err != nil {
		t.Fatalf("ResetGSTSettings(): %v", err)
	}
	if gst, _ := c.GSTSettings(ctx); gst.Config.Int("MaxTasks") != 0 {
		t.Errorf("after reset MaxTasks = %d, want the default", gst.Config.Int("MaxTasks"))
	}
}

func TestATorrServerWithoutGStreamerSaysSo(t *testing.T) {
	engine := &settingsEngine{}
	c := torrserver.NewClient(engine.serve(t).URL, nil)
	gst, err := c.GSTSettings(context.Background())
	if err != nil || gst.BuiltIn {
		t.Fatalf("GSTSettings() = %+v, %v; want built_in false", gst, err)
	}
	if err := c.UpdateGSTSettings(context.Background(), map[string]any{"MaxTasks": 1}); !errors.Is(err, torrserver.ErrNoGStreamer) {
		t.Errorf("UpdateGSTSettings() without GStreamer = %v, want ErrNoGStreamer", err)
	}
}

func TestTheMacAppsGStreamerIsUsedUnlessSomeoneChoseTheirOwn(t *testing.T) {
	const root = "/Users/me/Library/Application Support/moviestracker/gstreamer"
	ctx := context.Background()
	for _, tc := range []struct {
		name, before, want string
		changed            bool
	}{
		{"nothing set yet", "", root + "/1.28.7", true},
		{"already this one", root + "/1.28.7", root + "/1.28.7", false},
		{"an older download of the app", root + "/1.26.0", root + "/1.28.7", true},
		{"the person's own GStreamer", "/opt/gst", "/opt/gst", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := &settingsEngine{gstBuilt: true, gst: map[string]any{"GSTPath": tc.before, "MaxTasks": float64(2)}}
			c := torrserver.NewClient(engine.serve(t).URL, nil)
			changed, err := c.UseAppGStreamer(ctx, root+"/1.28.7", root)
			if err != nil || changed != tc.changed {
				t.Fatalf("UseAppGStreamer() = %v, %v; want changed %v", changed, err, tc.changed)
			}
			gst, _ := c.GSTSettings(ctx)
			if got := gst.Config.String("GSTPath"); got != tc.want || gst.Config.Int("MaxTasks") != 2 {
				t.Errorf("GSTPath = %q (MaxTasks %d), want %q and the rest kept", got, gst.Config.Int("MaxTasks"), tc.want)
			}
		})
	}

	engine := &settingsEngine{}
	c := torrserver.NewClient(engine.serve(t).URL, nil)
	if changed, err := c.UseAppGStreamer(ctx, root+"/1.28.7", root); changed || err != nil {
		t.Errorf("a TorrServer without GStreamer = %v, %v; want nothing to do", changed, err)
	}
}
