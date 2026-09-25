package engine_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/engine"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

// bigBuckBunny is a public-domain torrent, used to exercise add/list/remove.
const bigBuckBunny = "magnet:?xt=urn:btih:dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c&dn=Big+Buck+Bunny"

// TestTheClientSpeaksTheRealTorrServerAPI runs the pinned TorrServer (set
// TORRSERVER_BIN, e.g. after `make torrserver`) through the supervisor and
// checks the client against it, so API drift fails here instead of in use.
func TestTheClientSpeaksTheRealTorrServerAPI(t *testing.T) {
	binary := os.Getenv("TORRSERVER_BIN")
	if binary == "" {
		t.Skip("set TORRSERVER_BIN to the pinned TorrServer (make torrserver) to run the contract test")
	}
	sup := engine.New(engine.Config{Binary: binary, Dir: t.TempDir(), Port: freePort(t), ReadyTimeout: 60 * time.Second})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if err := sup.Start(ctx); err != nil {
		t.Fatalf("Start(): %v", err)
	}
	t.Cleanup(func() { _ = sup.Stop() })
	url, user, password := sup.Endpoint()
	mgr := torrserver.NewManager(url)
	if err := mgr.SetEndpoint(url, user, password); err != nil {
		t.Fatal(err)
	}
	client := mgr.Client()
	if code := get(t, url, "", ""); code != 401 {
		t.Errorf("TorrServer answered a settings read without credentials with %d, want 401", code)
	}
	if code := get(t, url, user, password+"x"); code != 401 {
		t.Errorf("TorrServer answered a wrong password with %d, want 401", code)
	}

	echo, err := client.Echo(ctx)
	if err != nil || !strings.HasPrefix(echo.Version, "MatriX.") {
		t.Fatalf("Echo() = %+v, %v", echo, err)
	}
	if size, err := client.CacheSize(ctx); err != nil || size != 64<<20 {
		t.Errorf("CacheSize() = %d, %v; want TorrServer's default 64 MB", size, err)
	}
	sets, err := client.Settings(ctx)
	if err != nil || sets.Int("ConnectionsLimit") != 25 || !sets.Bool("EnableBonjour") || sets.String("DefaultTrackers") == "" {
		t.Fatalf("Settings() of a fresh engine = %v, %v; want TorrServer's defaults", sets, err)
	}
	if err := client.UpdateSettings(ctx, map[string]any{"EnableBonjour": false, "ConnectionsLimit": 30}); err != nil {
		t.Fatalf("UpdateSettings(): %v", err)
	}
	after, err := client.Settings(ctx)
	if err != nil || after.Bool("EnableBonjour") || after.Int("ConnectionsLimit") != 30 || after.String("DefaultTrackers") != sets.String("DefaultTrackers") {
		t.Errorf("settings after update = %v, %v; want Bonjour off, 30 connections, trackers kept", after, err)
	}
	if gst, err := client.GSTSettings(ctx); err != nil {
		t.Errorf("GSTSettings(): %v", err)
	} else if gst.BuiltIn {
		if err := client.UpdateGSTSettings(ctx, map[string]any{"MaxTasks": 3}); err != nil {
			t.Errorf("UpdateGSTSettings(): %v", err)
		} else if again, _ := client.GSTSettings(ctx); again.Config.Int("MaxTasks") != 3 {
			t.Errorf("MaxTasks after update = %d, want 3", again.Config.Int("MaxTasks"))
		}
	}
	if list, err := client.ListTorrents(ctx); err != nil || len(list) != 0 {
		t.Fatalf("ListTorrents() on a fresh engine = %d torrents, %v", len(list), err)
	}

	if err := client.AddTorrent(ctx, bigBuckBunny, "Big Buck Bunny", "", "movie"); err != nil {
		t.Fatalf("AddTorrent(): %v", err)
	}
	const hash = "dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c"
	stats, err := client.TorrentStats(ctx, hash)
	if err != nil || stats.Title != "Big Buck Bunny" {
		t.Fatalf("TorrentStats() after add = %+v, %v", stats, err)
	}
	if err := client.DropTorrent(ctx, hash); err != nil {
		t.Errorf("DropTorrent(): %v", err)
	}
	if err := client.RemoveTorrent(ctx, hash); err != nil {
		t.Errorf("RemoveTorrent(): %v", err)
	}
	if _, err := client.TorrentStats(ctx, hash); !errors.Is(err, torrserver.ErrTorrentNotFound) {
		t.Errorf("TorrentStats() after remove: %v, want ErrTorrentNotFound", err)
	}
}
