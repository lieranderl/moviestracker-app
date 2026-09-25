package torrserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

func TestTorrServerClient_Echo(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/echo":
			_, _ = w.Write([]byte("MatriX.145"))
		case "/gst/echo":
			w.Header().Set("Content-Type", "application/json")
			// Shape of upstream /gst/echo (server/gstreamer/echo.go).
			_, _ = w.Write([]byte(`{"gst_discoverer":{"found":true,"available":true,"works":true},` +
				`"gstreamer":{"found":true,"available":true,"works":true,"version":"1.28.7"},` +
				`"hdr_tone_mapping":{"found":true,"available":true,"works":true},` +
				`"embedded_runtime":{"found":false,"available":false,"works":false}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	client := torrserver.NewClient(ts.URL, ts.Client())
	info, err := client.Echo(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if info.Version != "MatriX.145" {
		t.Errorf("expected version MatriX.145, got %q", info.Version)
	}
	if !info.GSTAvailable || info.GSTVersion != "1.28.7" {
		t.Errorf("expected GST available with version 1.28.7, got %+v", info)
	}
	if !info.Discoverer || !info.HDRTonemap {
		t.Errorf("expected gst-discoverer and HDR tone mapping reported as working, got %+v", info)
	}
}

func TestTorrServerClient_ListAndGetTorrents(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/torrents":
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			action, _ := req["action"].(string)
			switch action {
			case "list":
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`[{"hash":"abc123456","title":"Test Movie","stat_string":"Torrent working","torrent_size":1048576,"stat":3}]`))
			case "drop":
				w.WriteHeader(http.StatusOK)
			case "get":
				hash, _ := req["hash"].(string)
				if hash != "abc123456" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"hash":"abc123456","title":"Test Movie","stat_string":"Torrent in db","torrent_size":1048576,"stat":5,"total_peers":12,"pending_peers":5,"file_stats":[{"id":1,"path":"movie.mkv","length":1048576}]}`))
			default:
				http.Error(w, "unknown action", http.StatusBadRequest)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	client := torrserver.NewClient(ts.URL, ts.Client())
	torrents, err := client.ListTorrents(context.Background())
	if err != nil {
		t.Fatalf("ListTorrents failed: %v", err)
	}
	if len(torrents) != 1 || torrents[0].Hash != "abc123456" {
		t.Fatalf("expected 1 torrent with hash abc123456, got %+v", torrents)
	}
	// Verify that FileStats is NOT automatically fetched by ListTorrents (staying dropped)
	t0 := torrents[0]
	if len(t0.FileStats) != 0 {
		t.Errorf("expected empty file stats before user fetches files, got %+v", t0.FileStats)
	}

	// Also test explicit FetchTorrentFiles which populates cache and drops cache
	fetched, err := client.FetchTorrentFiles(context.Background(), "abc123456")
	if err != nil {
		t.Fatalf("FetchTorrentFiles failed: %v", err)
	}
	if len(fetched.FileStats) != 1 || fetched.FileStats[0].Path != "movie.mkv" {
		t.Fatalf("expected file movie.mkv from FetchTorrentFiles, got %+v", fetched.FileStats)
	}

	// Verify that subsequent ListTorrents now sees the cached file stats
	torrentsAfterFetch, err := client.ListTorrents(context.Background())
	if err != nil || len(torrentsAfterFetch) == 0 {
		t.Fatalf("ListTorrents after fetch failed: %v", err)
	}
	if len(torrentsAfterFetch[0].FileStats) != 1 || torrentsAfterFetch[0].FileStats[0].Path != "movie.mkv" {
		t.Errorf("expected cached file stats in ListTorrents after fetch, got %+v", torrentsAfterFetch[0].FileStats)
	}
}

func TestTorrServerClient_AddTorrent(t *testing.T) {
	var addedLink string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/torrents" {
			http.NotFound(w, r)
			return
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		action, _ := req["action"].(string)
		if action == "add" {
			addedLink, _ = req["link"].(string)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"hash":"xyz789"}`))
			return
		}
		if action == "drop" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "unknown action", http.StatusBadRequest)
	}))
	defer ts.Close()

	client := torrserver.NewClient(ts.URL, ts.Client())
	link := "magnet:?xt=urn:btih:xyz789&dn=Inception"
	if err := client.AddTorrent(context.Background(), link, "Inception", "", "movies"); err != nil {
		t.Fatalf("AddTorrent failed: %v", err)
	}
	if addedLink != link {
		t.Fatalf("expected added link %q, got %q", link, addedLink)
	}
}

func TestTorrServerClient_Actions(t *testing.T) {
	var droppedHash, remHash string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/torrents" {
			http.NotFound(w, r)
			return
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		action, _ := req["action"].(string)
		hash, _ := req["hash"].(string)
		switch action {
		case "drop":
			droppedHash = hash
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":"ok"}`))
		case "rem":
			remHash = hash
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":"ok"}`))
		default:
			http.Error(w, "bad action", http.StatusBadRequest)
		}
	}))
	defer ts.Close()

	client := torrserver.NewClient(ts.URL, ts.Client())
	if err := client.DropTorrent(context.Background(), "hash1"); err != nil {
		t.Fatalf("DropTorrent failed: %v", err)
	}
	if droppedHash != "hash1" {
		t.Fatalf("expected dropped hash1, got %q", droppedHash)
	}

	if err := client.RemoveTorrent(context.Background(), "hash2"); err != nil {
		t.Fatalf("RemoveTorrent failed: %v", err)
	}
	if remHash != "hash2" {
		t.Fatalf("expected rem hash2, got %q", remHash)
	}
}

func TestTorrServerClient_Probe(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gst/hash1/probe" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"DurationNS": 60000000000,
			"FileSize": 1048576,
			"Container": "Matroska",
			"Tracks": [
				{"Index": 0, "Type": "video", "CapsName": "video/x-h265", "Width": 1920, "Height": 1080},
				{"Index": 0, "Type": "audio", "CapsName": "audio/x-ac3", "Title": "English", "Language": "en"},
				{"Index": 0, "Type": "subtitle", "CapsName": "text/x-raw", "Title": "Full", "Language": "en"}
			]
		}`))
	}))
	defer ts.Close()

	client := torrserver.NewClient(ts.URL, ts.Client())
	res, err := client.Probe(context.Background(), "hash1", 1)
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}
	if res.Container != "Matroska" {
		t.Fatalf("expected container Matroska, got %q", res.Container)
	}
	if len(res.Tracks) != 3 {
		t.Fatalf("expected 3 tracks, got %d", len(res.Tracks))
	}
	if res.DurationFormatted() != "01:00" {
		t.Errorf("expected 01:00 duration, got %q", res.DurationFormatted())
	}
}

func TestTorrServerHelpers(t *testing.T) {
	if torrserver.FormatBytes(0) != "0 B" {
		t.Errorf("expected 0 B, got %s", torrserver.FormatBytes(0))
	}
	if !strings.Contains(torrserver.FormatBytes(1048576), "1.00 MB") {
		t.Errorf("expected 1.00 MB, got %s", torrserver.FormatBytes(1048576))
	}

	if !torrserver.IsVideoFile("test.mkv") || !torrserver.IsVideoFile("movie.mp4") {
		t.Errorf("expected video files to be recognized")
	}
	if torrserver.IsVideoFile("image.jpg") || torrserver.IsVideoFile("sub.srt") {
		t.Errorf("expected non-video files to not be recognized")
	}

	files := []torrserver.FileStat{
		{ID: 1, Path: "sample.mkv", Length: 100},
		{ID: 2, Path: "movie.mkv", Length: 5000},
		{ID: 3, Path: "sub.srt", Length: 10},
	}
	main := torrserver.MainVideoFile(files)
	if main == nil || main.ID != 2 {
		t.Fatalf("expected main video to be movie.mkv (ID 2), got %+v", main)
	}

	torr := torrserver.Torrent{
		Hash:       "abc123hash",
		Title:      "Dune Part Two",
		Connected:  15,
		TotalPeers: 50,
		Download:   5242880,
		Upload:     131072,
		FileStats:  files,
	}

	if torr.FormattedPeers() != "15 / 50" {
		t.Errorf("expected '15 / 50', got %q", torr.FormattedPeers())
	}
	if torr.FormattedDownloadSpeed() != "5.00 MB/s" {
		t.Errorf("expected '5.00 MB/s', got %q", torr.FormattedDownloadSpeed())
	}
	if torr.FormattedUploadSpeed() != "128.00 KB/s" {
		t.Errorf("expected '128.00 KB/s', got %q", torr.FormattedUploadSpeed())
	}
	if !strings.HasPrefix(torr.MagnetLink(), "magnet:?xt=urn:btih:abc123hash") {
		t.Errorf("expected valid magnet link, got %q", torr.MagnetLink())
	}
	vFiles := torr.VideoFiles()
	if len(vFiles) != 2 {
		t.Errorf("expected 2 video files, got %d", len(vFiles))
	}

	// Test zero values
	emptyTorr := torrserver.Torrent{}
	if emptyTorr.FormattedPeers() != "0" {
		t.Errorf("expected '0', got %q", emptyTorr.FormattedPeers())
	}
	if emptyTorr.FormattedDownloadSpeed() != "0 B/s" {
		t.Errorf("expected '0 B/s', got %q", emptyTorr.FormattedDownloadSpeed())
	}
	if emptyTorr.FormattedUploadSpeed() != "0 B/s" {
		t.Errorf("expected '0 B/s', got %q", emptyTorr.FormattedUploadSpeed())
	}
	if emptyTorr.MagnetLink() != "" {
		t.Errorf("expected empty magnet link, got %q", emptyTorr.MagnetLink())
	}
}

func TestTorrServerDisplayNameAndDataEnrichment(t *testing.T) {
	// Test DisplayName and MagnetLink fallbacks
	t1 := torrserver.Torrent{
		Hash:  "fc6ff111339de219fec8631c1aa1df472739ffb2",
		Title: "Custom Title",
		Name:  "Lanterns.S01",
	}
	if t1.DisplayName() != "Custom Title" {
		t.Errorf("expected 'Custom Title', got %q", t1.DisplayName())
	}

	t2 := torrserver.Torrent{
		Hash: "fc6ff111339de219fec8631c1aa1df472739ffb2",
		Name: "Lanterns.S01",
	}
	if t2.DisplayName() != "Lanterns.S01" {
		t.Errorf("expected 'Lanterns.S01', got %q", t2.DisplayName())
	}
	if !strings.Contains(t2.MagnetLink(), "dn=Lanterns.S01") {
		t.Errorf("expected magnet link to contain dn=Lanterns.S01, got %q", t2.MagnetLink())
	}

	t3 := torrserver.Torrent{
		Hash: "fc6ff111339de219fec8631c1aa1df472739ffb2",
	}
	if t3.DisplayName() != "fc6ff111339de219fec8631c1aa1df472739ffb2" {
		t.Errorf("expected hash as fallback, got %q", t3.DisplayName())
	}

	d := torrserver.TorrentDetails{
		Hash: "fc6ff111339de219fec8631c1aa1df472739ffb2",
		Name: "Lanterns.S01",
	}
	if d.DisplayName() != "Lanterns.S01" {
		t.Errorf("expected 'Lanterns.S01', got %q", d.DisplayName())
	}
	if !strings.Contains(d.MagnetLink(), "dn=Lanterns.S01") {
		t.Errorf("expected details magnet link to contain dn=Lanterns.S01, got %q", d.MagnetLink())
	}

	// Test Data JSON decoding for dropped torrents in ListTorrents
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{
				"hash": "fc6ff111339de219fec8631c1aa1df472739ffb2",
				"name": "Lanterns.S01",
				"stat": 5,
				"stat_string": "Torrent in db",
				"data": "{\"TorrServer\":{\"Files\":[{\"id\":1,\"path\":\"Lanterns.S01E01.mkv\",\"length\":1024000}]}}"
			}
		]`))
	}))
	defer ts.Close()

	client := torrserver.NewClient(ts.URL, ts.Client())
	list, err := client.ListTorrents(context.Background())
	if err != nil {
		t.Fatalf("ListTorrents failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 torrent, got %d", len(list))
	}
	if list[0].DisplayName() != "Lanterns.S01" {
		t.Errorf("expected display name 'Lanterns.S01', got %q", list[0].DisplayName())
	}
	if len(list[0].FileStats) != 1 || list[0].FileStats[0].Path != "Lanterns.S01E01.mkv" {
		t.Errorf("expected file Lanterns.S01E01.mkv parsed from data, got %+v", list[0].FileStats)
	}
}

// TorrServer answers /cache and /torrents "get" through torr.GetTorrent, which
// extends an active torrent's life (and starts a dropped one), so a page that
// merely shows the list must not call them.
func TestListingTorrentsLeavesIdleTorrentsFreeToDisconnect(t *testing.T) {
	var other []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/torrents" && strings.Contains(readBody(r), `"list"`) {
			// Upstream "list" already carries each active torrent's live Status().
			_, _ = w.Write([]byte(`[{"hash":"h1","title":"Working","stat":3,"stat_string":"Torrent working",` +
				`"download_speed":4194304,"upload_speed":65536,"connected_seeders":8,"active_peers":14,"total_peers":40,` +
				`"preloaded_bytes":1024,"loaded_size":2048,"file_stats":[{"id":1,"path":"movie.mkv","length":1048576}]},` +
				`{"hash":"h2","title":"Dropped","stat":5,"stat_string":"Torrent in db"}]`))
			return
		}
		other = append(other, r.URL.Path)
		http.NotFound(w, r)
	}))
	defer ts.Close()

	list, err := torrserver.NewClient(ts.URL, nil).ListTorrents(context.Background())
	if err != nil {
		t.Fatalf("ListTorrents(): %v", err)
	}
	if len(other) != 0 {
		t.Errorf("listing made per-torrent requests %q, want none", other)
	}
	if len(list) != 2 {
		t.Fatalf("got %d torrents, want 2", len(list))
	}
	w := list[0]
	if w.FormattedDownloadSpeed() != "4.00 MB/s" || w.FormattedUploadSpeed() != "64.00 KB/s" || w.FormattedPeers() != "8 / 40" {
		t.Errorf("live stats = %s down, %s up, %s peers; want 4.00 MB/s, 64.00 KB/s, 8 / 40",
			w.FormattedDownloadSpeed(), w.FormattedUploadSpeed(), w.FormattedPeers())
	}
	if len(w.FileStats) != 1 || w.FileStats[0].Path != "movie.mkv" {
		t.Errorf("files = %+v, want movie.mkv from the list", w.FileStats)
	}
}

func TestWatchingOneTorrentsStatsDoesNotWakeIt(t *testing.T) {
	var other []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := readBody(r)
		if r.URL.Path == "/torrents" && strings.Contains(body, `"list"`) {
			_, _ = w.Write([]byte(`[{"hash":"aaaa","stat":3,"download_speed":1024},` +
				`{"hash":"bbbb","title":"Dune","stat":3,"stat_string":"Torrent working","download_speed":558359.06,` +
				`"connected_seeders":5,"active_peers":7,"total_peers":30,"preloaded_bytes":33554432}]`))
			return
		}
		other = append(other, r.URL.Path+" "+body)
		http.NotFound(w, r)
	}))
	defer ts.Close()
	client := torrserver.NewClient(ts.URL, nil)

	stats, err := client.TorrentStats(context.Background(), "BBBB")
	if err != nil {
		t.Fatalf("TorrentStats(): %v", err)
	}
	if stats.Title != "Dune" || stats.FormattedPeers() != "5 / 30" || stats.FormattedDownloadSpeed() != "545.27 KB/s" {
		t.Errorf("stats = %q, %s peers, %s; want Dune, 5 / 30, 545.27 KB/s", stats.Title, stats.FormattedPeers(), stats.FormattedDownloadSpeed())
	}
	if _, err := client.TorrentStats(context.Background(), "cccc"); !errors.Is(err, torrserver.ErrTorrentNotFound) {
		t.Errorf("TorrentStats(unknown) error = %v, want ErrTorrentNotFound", err)
	}
	if len(other) != 0 {
		t.Errorf("stats made requests %q, want only the list", other)
	}
}

func TestCacheSizeFollowsATorrServerSettingsChange(t *testing.T) {
	var requests atomic.Int32
	var cacheSize atomic.Int64
	cacheSize.Store(64 << 20)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_, _ = fmt.Fprintf(w, `{"CacheSize":%d,"PreloadCache":50}`, cacheSize.Load())
	}))
	defer ts.Close()
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	client := torrserver.NewClient(ts.URL, nil, torrserver.WithClock(func() time.Time { return now }))

	size := func() int64 {
		t.Helper()
		n, err := client.CacheSize(context.Background())
		if err != nil {
			t.Fatalf("CacheSize(): %v", err)
		}
		return n
	}
	if got := size(); got != 64<<20 {
		t.Fatalf("CacheSize() = %d, want 64 MB", got)
	}
	cacheSize.Store(128 << 20) // changed in TorrServer's settings
	if got := size(); got != 64<<20 || requests.Load() != 1 {
		t.Fatalf("second read = %d after %d requests, want the cached 64 MB after 1: the player reads it every second", got, requests.Load())
	}
	now = now.Add(time.Minute)
	if got := size(); got != 128<<20 {
		t.Fatalf("CacheSize() a minute later = %d, want 128 MB", got)
	}
}

func readBody(r *http.Request) string {
	b, _ := io.ReadAll(r.Body)
	return string(b)
}

func TestMasterPlaylistDescribesHLSOutput(t *testing.T) {
	master := `#EXTM3U
#EXT-X-VERSION:7

#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",NAME="Forced",LANGUAGE="ru",DEFAULT=NO,AUTOSELECT=YES,FORCED=NO,URI="/gst/h/subs/0.m3u8"
#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",NAME="Full",LANGUAGE="en",DEFAULT=NO,AUTOSELECT=YES,FORCED=NO,URI="/gst/h/subs/1.m3u8"

#EXT-X-STREAM-INF:BANDWIDTH=38546893,AVERAGE-BANDWIDTH=25697929,RESOLUTION=3840x1606,FRAME-RATE=24,CODECS="hvc1.2.4.H150.B0,mp4a.40.2",VIDEO-RANGE=PQ,SUBTITLES="subs"
/gst/h/video.m3u8?audio=0
`
	got := torrserver.ParseMasterPlaylist(master)
	want := torrserver.HLSOutput{
		VideoCodec: "hvc1.2.4.H150.B0", AudioCodec: "mp4a.40.2",
		Width: 3840, Height: 1606, FrameRate: "24", VideoRange: "PQ",
		Bandwidth: 38546893, AverageBandwidth: 25697929, Subtitles: 2,
	}
	if got != want {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestEchoReportsAServerThatIsNotTorrServerAsOffline(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler()) // e.g. a mistyped endpoint
	defer ts.Close()

	info, err := torrserver.NewClient(ts.URL, nil).Echo(context.Background())
	if err == nil {
		t.Fatal("Echo() error = nil, want an error for HTTP 404")
	}
	if info.Version != "" {
		t.Errorf("Version = %q, want empty: an error page is not a version", info.Version)
	}
}

func TestProbeKeepsTheHashInsideItsPathSegment(t *testing.T) {
	var paths []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.EscapedPath())
		_, _ = w.Write([]byte(`{}`))
	}))
	defer ts.Close()
	client := torrserver.NewClient(ts.URL, nil)

	_, _ = client.Probe(context.Background(), "../settings?x=", 1)
	_ = client.Heartbeat(context.Background(), "../settings?x=")

	want := []string{"/gst/..%2Fsettings%3Fx=/probe", "/gst/..%2Fsettings%3Fx=/heartbeat"}
	if strings.Join(paths, " ") != strings.Join(want, " ") {
		t.Errorf("requested paths = %q, want %q", paths, want)
	}
}

func TestOversizedTorrServerResponsesAreRefused(t *testing.T) {
	// One torrent with a 33 MB title: past any
	// real list, so the client must stop reading instead of buffering it.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`[{"hash":"a","stat":5,"title":"`))
		chunk := []byte(strings.Repeat("x", 1<<20))
		for range 33 {
			_, _ = w.Write(chunk)
		}
		_, _ = w.Write([]byte(`"}]`))
	}))
	defer ts.Close()

	if _, err := torrserver.NewClient(ts.URL, nil).ListTorrents(context.Background()); err == nil {
		t.Fatal("ListTorrents() error = nil, want an error for an oversized response")
	}
}

func TestEveryRequestCarriesTheEngineCredentials(t *testing.T) {
	var unauthenticated []string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); !ok || user != "moviestracker" || pass != "s3cret" {
			unauthenticated = append(unauthenticated, r.URL.Path)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/echo":
			_, _ = w.Write([]byte("MatriX.145"))
		case "/torrents":
			_, _ = w.Write([]byte(`[]`))
		case "/settings":
			_, _ = w.Write([]byte(`{"CacheSize":1}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer ts.Close()

	c := torrserver.NewClient(ts.URL, nil, torrserver.WithBasicAuth("moviestracker", "s3cret"))
	ctx := context.Background()
	if _, err := c.Echo(ctx); err != nil {
		t.Errorf("Echo(): %v", err)
	}
	if _, err := c.ListTorrents(ctx); err != nil {
		t.Errorf("ListTorrents(): %v", err)
	}
	if _, err := c.CacheSize(ctx); err != nil {
		t.Errorf("CacheSize(): %v", err)
	}
	_ = c.AddTorrent(ctx, "magnet:?xt=urn:btih:aaaa", "", "", "")
	_ = c.Heartbeat(ctx, "aaaa")
	if len(unauthenticated) != 0 {
		t.Errorf("requests without credentials: %q", unauthenticated)
	}
}

// Fetching files wakes a torrent kept in TorrServer's database, so it is put
// back to sleep. Any other torrent is left as it was: one still getting info
// is not saved yet, and dropping it would remove it; one working may be playing.
func TestFetchingFilesOnlyPutsBackATorrentItWokeFromTheDatabase(t *testing.T) {
	for _, tc := range []struct {
		name     string
		torrent  string
		wantDrop bool
	}{
		{"getting info", `{"hash":"abc","stat":1,"stat_string":"Torrent getting info"}`, false},
		{"working", `{"hash":"abc","stat":3,"stat_string":"Torrent working","file_stats":[{"id":1,"path":"movie.mkv","length":1}]}`, false},
		{"in db", `{"hash":"abc","stat":5,"stat_string":"Torrent in db","file_stats":[{"id":1,"path":"movie.mkv","length":1}]}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dropped := false
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req map[string]any
				_ = json.NewDecoder(r.Body).Decode(&req)
				switch req["action"] {
				case "get":
					_, _ = w.Write([]byte(tc.torrent))
				case "drop":
					dropped = true
				}
			}))
			defer ts.Close()

			if _, err := torrserver.NewClient(ts.URL, ts.Client()).FetchTorrentFiles(context.Background(), "abc"); err != nil {
				t.Fatalf("FetchTorrentFiles(): %v", err)
			}
			if dropped != tc.wantDrop {
				t.Errorf("dropped = %v, want %v", dropped, tc.wantDrop)
			}
		})
	}
}
