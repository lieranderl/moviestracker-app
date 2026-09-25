package views_test

import (
	"fmt"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// testLinks stands in for the signed share links the server makes.
var testLinks = views.StreamLinks{
	Direct: func(hash string, f torrserver.FileStat) string {
		return fmt.Sprintf("/s/%s.%d/%s", hash, f.ID, torrserver.StreamFileName(hash, f))
	},
	HLS: func(hash string, f torrserver.FileStat) string {
		return fmt.Sprintf("/s/%s.%d/hls/master.m3u8", hash, f.ID)
	},
}

func torrPage(t *testing.T, gst bool) string {
	t.Helper()
	torrents := []torrserver.Torrent{{Hash: "hash1", Title: "Movie", FileStats: []torrserver.FileStat{{ID: 1, Path: "dir/movie.mkv", Length: 1 << 30}}}}
	return html.UnescapeString(render(t, views.TorrServer(testUser, "http://nas:8090", "http://192.168.1.20:8095", testLinks, torrserver.EchoInfo{Version: "1.0", GSTAvailable: gst}, torrents, views.GStreamerSetup{})))
}

func TestDirectAndHLSLinksAreLabelledAndKeptApart(t *testing.T) {
	out := torrPage(t, true)
	for _, want := range []string{
		`aria-label="Direct: the original file"`, `aria-label="HLS: converted by GStreamer"`,
		"For VLC, IINA, Infuse and TVs", "For Safari, iPhone, Apple TV",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	if n := strings.Count(out, `data-url="/s/hash1.1/movie.mkv"`); n < 2 {
		t.Errorf("direct link copy on the card and the file row: found %d", n)
	}
	if n := strings.Count(out, `data-url="/s/hash1.1/hls/master.m3u8"`); n < 2 {
		t.Errorf("HLS link copy on the card and the file row: found %d", n)
	}
	for _, kind := range []string{"direct", "hls"} {
		if !strings.Contains(out, "/api/torrserver/playlist?hash=hash1&kind="+kind) {
			t.Errorf("no %s playlist download", kind)
		}
	}
	if !strings.Contains(out, `data-clean="/s/hash1.1/movie.mkv"`) || !strings.Contains(out, `data-hls="/s/hash1.1/hls/master.m3u8"`) {
		t.Error("play buttons should carry both links for the player")
	}
	// Links for other devices never need this browser's sign-in.
	if regexp.MustCompile(`data-url="/api/`).MatchString(out) || strings.Contains(out, "Copy Playlist URL") {
		t.Error("a copy button hands out an address that needs signing in")
	}
}

func TestHLSControlsShowOnlyWithGStreamer(t *testing.T) {
	for _, gst := range []bool{true, false} {
		want := "gst: false"
		if gst {
			want = "gst: true"
		}
		if !strings.Contains(torrPage(t, gst), want) {
			t.Errorf("page signals should start with %q", want)
		}
	}
	out := torrPage(t, true)
	hls := regexp.MustCompile(`<[a-z]+[^>]*(?:aria-label="HLS: converted by GStreamer"|data-action="probe")[^>]*>`).FindAllString(out, -1)
	if len(hls) < 4 {
		t.Fatalf("expected HLS groups on card, file row, footer, player and a probe button; found %d", len(hls))
	}
	for _, m := range hls {
		if !strings.Contains(m, `data-show="$gst"`) {
			t.Errorf("HLS control must be gated by $gst: %s", m)
		}
	}
}

func TestTorrentListUsesTwoColumnsOnXL(t *testing.T) {
	out := render(t, views.TorrServerLiveFragment(nil, torrserver.EchoInfo{}, "http://a", testLinks, false))
	if !regexp.MustCompile(`id="torr-list-container" class="[^"]*xl:grid-cols-2`).MatchString(out) {
		t.Fatalf("torrent list should render two columns on xl screens")
	}
}

func TestPlayerOffersAudioPickerSeededFromRememberedLanguage(t *testing.T) {
	out := torrPage(t, true)
	if !strings.Contains(out, `id="torr-audio-picker"`) {
		t.Errorf("player modal should hold the audio picker slot")
	}
	if !strings.Contains(out, `audioLang: localStorage.getItem('torrAudioLang') || ''`) {
		t.Errorf("audio language preference should be seeded from localStorage")
	}
	if !strings.Contains(out, `if ($playKind === 'hls') {
		@get('/api/torrserver/tracks?hash=' + h + '&index=' + $activeFileIndex`) {
		t.Errorf("playing a file through GStreamer should fetch its audio tracks")
	}
}

func TestPlayerStatsStreamOnlyWhileThePlayerIsOpen(t *testing.T) {
	out := torrPage(t, true)
	if !strings.Contains(out, `id="torr-player-stats"`) {
		t.Errorf("player modal should hold the stats row")
	}
	if !strings.Contains(out, `data-effect="$playerOpen && $activeHash`) ||
		!strings.Contains(out, `@get('/api/torrserver/player-stats?stop=1'`) {
		t.Errorf("the stats stream should run only while the player is open and end when it closes")
	}
}

func TestMediaInfoPanelToggles(t *testing.T) {
	out := torrPage(t, true)
	if !strings.Contains(out, `data-on:click="$mediaInfoOpen = !$mediaInfoOpen"`) {
		t.Errorf("player should offer a Media Info toggle")
	}
	if !strings.Contains(out, `data-show="$playKind === 'hls' && $mediaInfoOpen"`) {
		t.Errorf("media info panel should only show when toggled open while HLS plays")
	}
}

func TestPlayerControlsLiveInsideFullscreenContainer(t *testing.T) {
	out := torrPage(t, true)
	video := videoTag(out)
	if regexp.MustCompile(`\scontrols[\s>]`).MatchString(video) {
		t.Errorf("native controls hide our menus in fullscreen; video should not use them: %s", video)
	}
	if !strings.Contains(video, `data-ref:_video`) || !strings.Contains(video, `data-on:torr-subtitle="$subTrack = evt.detail"`) {
		t.Errorf("video should expose itself to the control bar and report subtitle changes: %s", video)
	}
	start := strings.Index(out, `data-ref:_player`)
	end := strings.Index(out, `id="torr-player-stats"`)
	if start < 0 || end < start {
		t.Fatalf("player container with data-ref:_player should precede the stats row")
	}
	player := out[start:end]
	for _, want := range []string{
		`id="torr-audio-picker"`,
		`id="torr-subtitle-menu"`,
		`$_player.requestFullscreen()`,
		`aria-label="Play or pause"`,
		`aria-label="Seek"`,
	} {
		if !strings.Contains(player, want) {
			t.Errorf("expected %s inside the fullscreen player container", want)
		}
	}
}

// Streams end cleanly when the server shuts down; Datastar only reconnects
// after a clean end with retry: 'always', so the live views survive deploys.
func TestLiveStreamsReconnectAfterAServerRestart(t *testing.T) {
	pages := map[string]string{
		"torrserver": torrPage(t, false),
	}
	for name, body := range pages {
		if !strings.Contains(html.UnescapeString(body), "?stream=true', {retry: 'always'})") {
			t.Errorf("%s page: stream is not reopened after the server ends it", name)
		}
	}
}

func TestSetupGuideExplainsHowToInstallTorrServerWhenItIsOffline(t *testing.T) {
	out := html.UnescapeString(render(t, views.TorrServerGuide(torrserver.EchoInfo{}, "http://127.0.0.1:8090", false)))
	for _, want := range []string{
		"bash ./installTorrServerMac.sh --install --gst", // macOS, GStreamer build
		"brew install gstreamer",                         // macOS GStreamer runtime
		"TorrServer-gst-windows-amd64.exe",               // Windows build with GStreamer built in
		"http://127.0.0.1:8090",                          // where to check it runs
		"Why GStreamer",
		"https://github.com/YouROK/TorrServer#gstreamer",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("offline guide lacks %q", want)
		}
	}
}

func TestSetupGuideSuggestsTheGStreamerBuildWhenMissing(t *testing.T) {
	out := html.UnescapeString(render(t, views.TorrServerGuide(torrserver.EchoInfo{Version: "MatriX.145"}, "http://nas:8090", false)))
	for _, want := range []string{"GStreamer build", "--install --gst", "TorrServer-gst-windows-amd64.exe", "VLC"} {
		if !strings.Contains(out, want) {
			t.Errorf("no-GStreamer hint lacks %q", want)
		}
	}
	if strings.Contains(out, "Set up TorrServer") {
		t.Error("TorrServer is running: the install guide should not be shown")
	}
}

func TestSetupGuideIsHiddenWhenGStreamerWorks(t *testing.T) {
	out := render(t, views.TorrServerGuide(torrserver.EchoInfo{Version: "MatriX.145", GSTAvailable: true}, "http://nas:8090", false))
	if strings.Contains(out, "install") || !strings.Contains(out, `id="torr-setup-guide"`) {
		t.Errorf("want only the empty guide anchor, got:\n%s", out)
	}
}

// Cards side by side share a row height, and their titles, action buttons and
// file rows line up however long each title is.
func TestTorrentCardsInARowLineUp(t *testing.T) {
	torrents := []torrserver.Torrent{
		{Hash: "a", Title: "Short"},
		{Hash: "b", Title: "A much longer release title that wraps onto a second line", FileStats: []torrserver.FileStat{{ID: 1, Path: "v.mkv"}}},
	}
	out := render(t, views.TorrServerListFragment(testLinks, torrents))
	if regexp.MustCompile(`id="torr-list-container" class="[^"]*items-start`).MatchString(out) {
		t.Error("list grid must stretch cards to the row height, not items-start")
	}
	if n := strings.Count(out, "min-h-[2lh]"); n != 2 {
		t.Errorf("titles reserving two lines = %d, want 2 (keeps buttons aligned)", n)
	}
	if n := strings.Count(out, "mt-auto"); n != 2 {
		t.Errorf("file rows pinned to the card bottom = %d, want 2", n)
	}
}

func TestThePlayerNoLongerPollsFromTheBrowser(t *testing.T) {
	out := torrPage(t, true)
	// Timers may tidy the page (the double-tap hint), never ask the server.
	for _, timer := range regexp.MustCompile(`data-on-interval[^=]*="[^"]*"`).FindAllString(out, -1) {
		if strings.Contains(timer, "@get") || strings.Contains(timer, "@post") {
			t.Errorf("the TorrServer page still polls the server: %s", timer)
		}
	}
	if !strings.Contains(out, "/api/torrserver/player-stats?stream=true") {
		t.Error("the player does not open a stats stream")
	}
}

func TestMediaInfoSaysWhetherHDRIsKeptOrToneMapped(t *testing.T) {
	video := func(transfer string, mastering bool) *torrserver.ProbeResult {
		return &torrserver.ProbeResult{Container: "Matroska", Tracks: []torrserver.ProbeTrack{
			{Type: "video", CapsName: "video/x-h265", VideoTransfer: transfer, HasMasteringDisplayInfo: mastering, Width: 3840, Height: 2072},
		}}
	}
	for _, tc := range []struct {
		name   string
		probe  *torrserver.ProbeResult
		output string // VIDEO-RANGE of the HLS master playlist
		want   string
	}{
		{"HDR10 remuxed", video("pq", true), "PQ", "HDR10 · Kept"},
		{"PQ without mastering data", video("pq", false), "PQ", "PQ HDR · Kept"},
		{"HLG remuxed", video("hlg", false), "HLG", "HLG · Kept"},
		{"SDR stays SDR", video("", false), "", "SDR"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := html.UnescapeString(render(t, views.TorrMediaInfo(tc.probe, 0, &torrserver.HLSOutput{VideoCodec: "hvc1.2.4.L153", Width: 3840, Height: 2072, VideoRange: tc.output})))
			if !strings.Contains(out, ">"+tc.want+"<") {
				t.Errorf("HDR output lacks %q:\n%s", tc.want, out)
			}
		})
	}
}

func TestDoubleTappingTheVideoSidesSeeksTenSeconds(t *testing.T) {
	out := torrPage(t, true)
	video := videoTag(out)
	for _, want := range []string{
		"evt.offsetX / el.clientWidth", // where on the video
		"x < 1 / 3", "x > 2 / 3",       // left third back, right third forward
		"el.currentTime + step",
		"document.fullscreenElement", // the middle still toggles fullscreen
		"touch-manipulation",         // no double-tap zoom over the video
	} {
		if !strings.Contains(video, want) {
			t.Errorf("video double tap lacks %q:\n%s", want, video)
		}
	}
	for _, want := range []string{`data-show="$_seekHint === 'back' ||`, `data-show="$_seekHint === 'fwd' ||`, "_seekHint: ''"} {
		if !strings.Contains(out, want) {
			t.Errorf("player lacks the seek feedback %q", want)
		}
	}
}

// videoTag is the player's <video> start tag; its attributes hold Datastar
// expressions that may contain ">", so it ends at the element's close.
func videoTag(page string) string {
	start := strings.Index(page, `<video`)
	end := strings.Index(page, `></video>`)
	if start < 0 || end < start {
		return ""
	}
	return page[start:end]
}

func TestOnlyTheCentreOfTheVideoPlaysAndPausesAndHoveringShowsWhatEachPartDoes(t *testing.T) {
	out := torrPage(t, true)
	video := videoTag(out)
	click := regexp.MustCompile(`data-on:click="([^"]*)"`).FindStringSubmatch(video)
	if click == nil || !strings.Contains(click[1], "x >= 1 / 3 && x <= 2 / 3") || !strings.Contains(click[1], "el.paused ? el.play() : el.pause()") {
		t.Errorf("a click should play or pause only in the centre third: %q", click)
	}
	for _, want := range []string{`data-on:pointermove="$_zone =`, `data-on:pointerleave="$_zone = ''"`} {
		if !strings.Contains(video, want) {
			t.Errorf("video lacks %s", want)
		}
	}
	for _, want := range []string{`aria-hidden="true" data-show="$_paused || ($_zone === 'center' && !$_idle)"`, "$_zone === 'back'", "$_zone === 'fwd'"} {
		if !strings.Contains(out, want) {
			t.Errorf("player lacks the hover hint %q", want)
		}
	}
}

func TestEachStreamKindHasItsOwnPlayButtonAndThePlayerSwitchesBetweenThem(t *testing.T) {
	out := torrPage(t, true)
	plays := regexp.MustCompile(`<button[^>]*data-action="play"[^>]*>`).FindAllString(out, -1)
	kinds := map[string]int{}
	for _, b := range plays {
		m := regexp.MustCompile(`data-kind="(direct|hls)"`).FindStringSubmatch(b)
		if m == nil {
			t.Errorf("a play button without a stream kind: %s", b)
			continue
		}
		kinds[m[1]]++
	}
	if kinds["direct"] < 2 || kinds["hls"] < 2 {
		t.Errorf("want Direct and HLS play on the card and the file row, got %v", kinds)
	}
	// The HLS play sits inside the HLS group, which needs GStreamer.
	group := regexp.MustCompile(`(?s)<div role="group" aria-label="HLS: converted by GStreamer"[^>]*>.*?</div>`).FindString(out)
	if !strings.Contains(group, `data-kind="hls"`) || !strings.Contains(group, `data-show="$gst"`) {
		t.Errorf("HLS play belongs in the GStreamer-gated HLS group: %s", group)
	}
	if !strings.Contains(out, `$playKind = d.kind === 'hls' && $gst ? 'hls' : 'direct'`) {
		t.Error("playing should follow the button's kind, falling back to Direct without GStreamer")
	}
	for _, kind := range []string{"direct", "hls"} {
		if !strings.Contains(out, fmt.Sprintf(`aria-label="Play %s"`, map[string]string{"direct": "Direct", "hls": "HLS"}[kind])) {
			t.Errorf("player lacks a switch to %s", kind)
		}
	}
	if !strings.Contains(out, `data-show="$playKind === 'hls' && $mediaInfoOpen"`) {
		t.Error("media info describes the GStreamer output, so only while HLS plays")
	}
}

func TestTheMacAppsTorrServerOffersGStreamerInsteadOfAnotherBuild(t *testing.T) {
	gst := views.GStreamerSetup{Can: true, Admin: true, Pinned: "1.28.7", Size: "146 MB"}
	guide := html.UnescapeString(render(t, views.TorrServerGuide(torrserver.EchoInfo{Version: "MatriX.145"}, "http://127.0.0.1:18090", true)))
	if strings.Contains(guide, "GStreamer build") || strings.Contains(guide, "brew install") {
		t.Errorf("Moviestracker's own TorrServer is the GStreamer build: no install advice, got:\n%s", guide)
	}
	page := html.UnescapeString(render(t, views.TorrServer(testUser, "http://127.0.0.1:18090", "http://192.168.1.20:8095", testLinks, torrserver.EchoInfo{Version: "MatriX.145"}, nil, gst)))
	if !strings.Contains(page, "Install GStreamer (146 MB)") || !strings.Contains(page, "/api/gstreamer?compact=1") {
		t.Error("the TorrServer page should offer GStreamer, kept current by its own stream")
	}
}

func TestDirectAndHLSExplainThemselvesWithTooltips(t *testing.T) {
	out := torrPage(t, true)
	for _, tip := range []string{
		`data-tip="The original file, full quality: for VLC, IINA, Infuse and TVs"`,
		`data-tip="Converted by GStreamer as it plays: for Safari, iPhone, Apple TV and TV browsers"`,
	} {
		if n := strings.Count(out, tip); n < 3 {
			t.Errorf("%s shown %d times, want on the card, the file row and the player", tip, n)
		}
	}
	// The whole Direct or HLS group explains itself, in its own colour.
	// (The player's link rows spell it out next to them instead.)
	groups := regexp.MustCompile(`<div role="group" aria-label="(?:Direct|HLS): [^"]*"[^>]*rounded-field[^>]*>`).FindAllString(out, -1)
	if len(groups) < 4 {
		t.Errorf("found %d Direct/HLS groups, want the card's and the file row's", len(groups))
	}
	for _, g := range groups {
		if !strings.Contains(g, "data-tip=") || !regexp.MustCompile(`tooltip-(primary|secondary)`).MatchString(g) {
			t.Errorf("stream group without a coloured tooltip: %s", g)
		}
	}
	for _, label := range []string{`aria-label="Play Direct"`, `aria-label="Play HLS"`} {
		sw := regexp.MustCompile(`<button[^>]*join-item[^>]*` + label + `[^>]*>|<button[^>]*` + label + `[^>]*join-item[^>]*>`).FindString(out)
		if !strings.Contains(sw, "tooltip") || !strings.Contains(sw, "data-tip=") {
			t.Errorf("the player's switch lacks a tooltip: %s", sw)
		}
	}
	// On phones there is no hover: the list starts with a visible explainer.
	legend := regexp.MustCompile(`(?s)<div id="stream-kinds".*?</div>\s*</div>\s*</div>`).FindString(out)
	for _, want := range []string{"Direct", "The original file, full quality", "HLS", "Converted by GStreamer as it plays"} {
		if !strings.Contains(legend, want) {
			t.Errorf("stream kinds explainer lacks %q:\n%s", want, legend)
		}
	}
}
