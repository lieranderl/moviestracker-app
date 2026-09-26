package handlers

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	playback "github.com/lieranderl/moviestracker-app/internal/streams"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"

	"github.com/a-h/templ"
	"github.com/starfederation/datastar-go/datastar"
)

// handleTorrServerPage serves the protected TorrServer web interface.
func (s *Server) handleTorrServerPage(w http.ResponseWriter, r *http.Request) {
	user := s.userFromRequest(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	client := s.torrServer.Client()
	echo, _ := client.Echo(ctx)
	torrents, _ := client.ListTorrents(ctx)
	s.matchTitles(torrents)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.TorrServer(user, s.engineLabel(), s.linkOrigin(r), s.streamLinks(), echo, torrents, s.gstSetup(user, s.gstStatus(), echo)).Render(r.Context(), w); err != nil {
		slog.Error("failed to render TorrServer view", "error", err)
	}
}

// handleTorrServerTorrents streams or snapshots the active torrents list.
func (s *Server) handleTorrServerTorrents(w http.ResponseWriter, r *http.Request) {
	user := s.userFromRequest(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	stream := r.URL.Query().Get("stream") == "true"
	if stream {
		if !s.acquireSSE() {
			w.Header().Set("Retry-After", "2")
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		defer s.releaseSSE()
	}

	sse := datastar.NewSSE(w, r)
	if !stream {
		ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
		defer cancel()
		client := s.torrServer.Client()
		torrents, _ := client.ListTorrents(ctx)
		echo, _ := client.Echo(ctx)
		if _, err := s.patchTorrLiveIfChanged(r.Context(), sse, "", torrents, echo); err != nil {
			logSSEError(r, "patch torrserver list snapshot", err)
		}
		return
	}

	// A stream starts from the shared snapshots, so reconnects (a hidden tab,
	// a flaky network) never reach TorrServer: the shared pollers keep the
	// list and the engine current for every open page.
	streamCtx, streamCancel := s.streamContext(r)
	defer streamCancel()
	sub := s.live.Subscribe(topicEngine, topicTorrents)
	defer sub.Close()
	var last string
	for {
		select {
		case <-streamCtx.Done():
			return
		case <-sub.C:
		}
		if sub.Snapshot(topicEngine).Version == 0 || sub.Snapshot(topicTorrents).Version == 0 {
			continue // not both known yet: rendering now would flash "offline"
		}
		tList := liveValue[torrentsState](sub, topicTorrents).List
		echoInfo := liveValue[torrserver.EchoInfo](sub, topicEngine)
		var err error
		if last, err = s.patchTorrLiveIfChanged(streamCtx, sse, last, tList, echoInfo); err != nil {
			logSSEError(r, "patch torrserver list stream", err)
			return
		}
	}
}

// torrLiveFragment renders every server-owned region of the TorrServer page.
func (s *Server) torrLiveFragment(torrents []torrserver.Torrent, echo torrserver.EchoInfo) templ.Component {
	s.matchTitles(torrents)
	return views.TorrServerLiveFragment(torrents, echo, s.engineLabel(), s.streamLinks(), s.gstCan())
}

// patchTorrLive patches every server-owned region plus the $gst capability
// signal that shows/hides GStreamer-only controls.
func (s *Server) patchTorrLive(sse *datastar.ServerSentEventGenerator, torrents []torrserver.Torrent, echo torrserver.EchoInfo) error {
	if err := sse.PatchElementTempl(s.torrLiveFragment(torrents, echo)); err != nil {
		return err
	}
	return patchGSTSignal(sse, echo)
}

func patchGSTSignal(sse *datastar.ServerSentEventGenerator, echo torrserver.EchoInfo) error {
	return sse.MarshalAndPatchSignals(map[string]any{"gst": echo.GSTAvailable})
}

// patchTorrLiveIfChanged renders the live fragment and patches it only when it
// differs from the previously sent HTML. It returns the HTML now on the client.
func (s *Server) patchTorrLiveIfChanged(ctx context.Context, sse *datastar.ServerSentEventGenerator, last string, torrents []torrserver.Torrent, echo torrserver.EchoInfo) (string, error) {
	var buf strings.Builder
	if err := s.torrLiveFragment(torrents, echo).Render(ctx, &buf); err != nil {
		return last, err
	}
	html := buf.String()
	if html == last {
		return last, nil
	}
	if err := sse.PatchElements(html); err != nil {
		return last, err
	}
	if err := patchGSTSignal(sse, echo); err != nil {
		return last, err
	}
	return html, nil
}

// Limits of one Add Torrents form.
const (
	maxAddFiles     = 20
	maxTorrentBytes = 4 << 20 // a .torrent of a large season pack is a few MB
	maxAddBytes     = maxAddFiles*maxTorrentBytes + 1<<20
)

// torrentHash is an info-hash on its own: 40 hex or 32 base32 characters.
var torrentHash = regexp.MustCompile(`^(?i:[0-9a-f]{40}|[a-z2-7]{32})$`)

// torrentLink reports whether TorrServer may be given link: a magnet, an
// http(s) link to a .torrent, a torrs:// link or an info-hash. Anything else
// (file:// makes TorrServer read a path on this machine) is refused.
func torrentLink(link string) bool {
	if torrentHash.MatchString(link) {
		return true
	}
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "magnet":
		return strings.HasPrefix(strings.ToLower(link), "magnet:?")
	case "http", "https":
		return u.Host != ""
	case "torrs":
		return true
	}
	return false
}

// handleTorrServerAdd adds the torrents of the Add Torrents form: links (one
// per line) and .torrent files. The title names the torrent when only one is
// added.
func (s *Server) handleTorrServerAdd(w http.ResponseWriter, r *http.Request) {
	if s.apiUser(w, r) == nil {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAddBytes)
	// Up to 8 MB in memory; the rest of the (bounded) body waits in temp files.
	if err := r.ParseMultipartForm(8 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) { // #nosec G120 -- the body is capped by MaxBytesReader above
		http.Error(w, "The torrents are too large to add at once.", http.StatusRequestEntityTooLarge)
		return
	}
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}
	var links, refused []string
	for _, line := range strings.FieldsFunc(r.FormValue("addLinks"), func(c rune) bool { return c == '\n' || c == '\r' }) {
		line = strings.Trim(strings.TrimSpace(line), `"'`)
		switch {
		case line == "":
		case torrentLink(line):
			links = append(links, line)
		default:
			refused = append(refused, line)
		}
	}
	type upload struct {
		name string
		data []byte
	}
	var files []upload
	if r.MultipartForm != nil {
		for i, fh := range r.MultipartForm.File["addFiles"] {
			if i >= maxAddFiles {
				refused = append(refused, fh.Filename+" (more than "+strconv.Itoa(maxAddFiles)+" files)")
				continue
			}
			data, err := readTorrentFile(fh)
			if err != nil {
				refused = append(refused, fh.Filename)
				continue
			}
			files = append(files, upload{fh.Filename, data})
		}
	}

	title := ""
	if len(links)+len(files) == 1 {
		title = strings.TrimSpace(r.FormValue("addTitle"))
	}
	client := s.torrServer.Client()
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	added := 0
	var failed []string
	for _, link := range links {
		if err := client.AddTorrent(ctx, link, title, "", ""); err != nil {
			slog.Warn("TorrServer did not add a torrent link", "error", err)
			failed = append(failed, link)
			continue
		}
		added++
	}
	for _, f := range files {
		if err := client.UploadTorrent(ctx, f.name, f.data, title); err != nil {
			slog.Warn("TorrServer did not add a torrent file", "file", logValue(f.name), "error", logError(err))
			failed = append(failed, f.name)
			continue
		}
		added++
	}

	if r.Header.Get("Datastar-Request") != "true" {
		http.Redirect(w, r, "/torrserver", http.StatusSeeOther)
		return
	}
	msg, kind := addResult(added, refused, failed)
	sse := datastar.NewSSE(w, r)
	if added > 0 {
		list, _ := client.ListTorrents(ctx)
		echo, _ := client.Echo(ctx)
		_ = s.patchTorrLive(sse, list, echo)
	}
	_ = sse.PatchElementTempl(views.TorrServerAlertFragment(msg, kind))
	if added > 0 && len(refused)+len(failed) == 0 {
		// A fresh form clears the chosen files too.
		_ = sse.PatchElementTempl(views.TorrAddForm())
		_ = sse.MarshalAndPatchSignals(map[string]any{"addModalOpen": false})
	}
}

// readTorrentFile reads an uploaded .torrent, refusing what is not one: a
// torrent file is a bencoded dictionary, so it starts with "d".
func readTorrentFile(fh *multipart.FileHeader) ([]byte, error) {
	if fh.Size > maxTorrentBytes {
		return nil, errors.New("too large")
	}
	f, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxTorrentBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxTorrentBytes || data[0] != 'd' {
		return nil, errors.New("not a torrent file")
	}
	return data, nil
}

// addResult tells what an Add Torrents form did, in one sentence or two.
func addResult(added int, refused, failed []string) (string, string) {
	var parts []string
	switch {
	case added == 1:
		parts = append(parts, "Added 1 torrent to TorrServer.")
	case added > 1:
		parts = append(parts, fmt.Sprintf("Added %d torrents to TorrServer.", added))
	default:
		parts = append(parts, "Nothing was added.")
	}
	if len(refused) > 0 {
		parts = append(parts, "Not torrent links or .torrent files: "+strings.Join(refused, ", ")+". Use magnet, http(s) or torrs links, info-hashes or .torrent files.")
	}
	if len(failed) > 0 {
		parts = append(parts, "TorrServer could not add: "+strings.Join(failed, ", ")+".")
	}
	switch {
	case added > 0 && len(refused)+len(failed) == 0:
		return strings.Join(parts, " "), "success"
	case added > 0:
		return strings.Join(parts, " "), "warning"
	}
	if len(refused)+len(failed) == 0 {
		parts = append(parts, "Enter at least one magnet link or torrent URL, or choose a .torrent file.")
	}
	return strings.Join(parts, " "), "error"
}

// handleTorrServerAction performs actions like "rem" or "drop".
func (s *Server) handleTorrServerAction(w http.ResponseWriter, r *http.Request) {
	user := s.userFromRequest(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	op := r.URL.Query().Get("op")
	hash := r.URL.Query().Get("hash")
	if hash == "" {
		http.Error(w, "Missing torrent hash", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	client := s.torrServer.Client()

	var err error
	var failure string
	switch op {
	case "drop":
		if gettingInfo(ctx, client, hash) {
			patchTorrAlert(r, datastar.NewSSE(w, r), "This torrent is still getting its info from peers: stopping it now would remove it. To get rid of it, an administrator can remove it.", "warning")
			return
		}
		err, failure = client.DropTorrent(ctx, hash), "TorrServer could not stop this torrent. Check that it is running."
	case "rem":
		if !user.IsAdmin() {
			http.Error(w, "Only an administrator can remove a torrent.", http.StatusForbidden)
			return
		}
		err, failure = client.RemoveTorrent(ctx, hash), "TorrServer could not remove this torrent. Check that it is running."
		if err == nil {
			s.forgetTitle(hash)
		}
	default:
		http.Error(w, "Invalid action operation", http.StatusBadRequest)
		return
	}

	sse := datastar.NewSSE(w, r)
	if err != nil {
		slog.Warn("torrserver action failed", "op", logValue(op), "hash", logValue(hash), "error", logError(err))
		patchTorrAlert(r, sse, failure, "error")
		return
	}
	tList, _ := client.ListTorrents(ctx)
	echoInfo, _ := client.Echo(ctx)
	if err := s.patchTorrLive(sse, tList, echoInfo); err != nil {
		logSSEError(r, "patch torrserver list after action", err)
	}
}

// gettingInfo reports whether TorrServer lists the torrent as still getting
// its info. The list does not wake torrents kept in the database.
func gettingInfo(ctx context.Context, client *torrserver.Client, hash string) bool {
	torrents, err := client.ListTorrents(ctx)
	if err != nil {
		return false
	}
	i := slices.IndexFunc(torrents, func(t torrserver.Torrent) bool { return strings.EqualFold(t.Hash, hash) })
	return i >= 0 && torrents[i].GettingInfo()
}

// patchTorrAlert shows a dismissible alert on the TorrServer page.
func patchTorrAlert(r *http.Request, sse *datastar.ServerSentEventGenerator, message, kind string) {
	if err := sse.PatchElementTempl(views.TorrServerAlertFragment(message, kind)); err != nil {
		logSSEError(r, "patch torrserver alert", err)
	}
}

// handleTorrServerFiles inspects/fetches files within a torrent upon user request.
func (s *Server) handleTorrServerFiles(w http.ResponseWriter, r *http.Request) {
	user := s.userFromRequest(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	hash := r.URL.Query().Get("hash")
	if hash == "" {
		http.Error(w, "Missing hash parameter", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	client := s.torrServer.Client()
	details, err := client.FetchTorrentFiles(ctx, hash)
	sse := datastar.NewSSE(w, r)
	if err != nil {
		slog.Warn("failed to get torrent files", "hash", logValue(hash), "error", logError(err))
		patchTorrAlert(r, sse, "TorrServer could not load the files of this torrent. Try again once it has metadata.", "error")
		return
	}

	// Re-render the live fragment so the card shows the fetched files accordion.
	if tList, errList := client.ListTorrents(ctx); errList == nil {
		echoInfo, _ := client.Echo(ctx)
		_ = s.patchTorrLive(sse, tList, echoInfo)
	}
	if len(details.FileStats) == 0 {
		_ = sse.PatchElementTempl(views.TorrServerAlertFragment(details.DisplayName()+" is still getting its info from peers; its files show up once it has it.", "info"))
		return
	}
	msg := fmt.Sprintf("Loaded %d file(s) for %s", len(details.FileStats), details.DisplayName())
	_ = sse.PatchElementTempl(views.TorrServerAlertFragment(msg, "success"))
}

// handleTorrServerProbe inspects GStreamer tracks.
func (s *Server) handleTorrServerProbe(w http.ResponseWriter, r *http.Request) {
	user := s.userFromRequest(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	hash := r.URL.Query().Get("hash")
	idxStr := r.URL.Query().Get("index")
	idx, _ := strconv.Atoi(idxStr)
	if idx <= 0 {
		idx = 1
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	probe, err := s.torrServer.Client().Probe(ctx, hash, idx)
	if err != nil {
		slog.Warn("probe failed", "hash", logValue(hash), "index", idx, "error", logError(err))
		sse := datastar.NewSSE(w, r)
		_ = sse.PatchElementTempl(views.TorrServerProbeError("Probe failed or media is still buffering. Try again in a few seconds."))
		return
	}

	sse := datastar.NewSSE(w, r)
	_ = sse.PatchElementTempl(views.TorrServerProbeFragment(probe))
}

// handleTorrServerTracks lists a file's audio and subtitle tracks for the player and picks
// the one matching the viewer's remembered language. Patching $audioTrack is
// what starts playback, so a failed probe still answers with track 0.
func (s *Server) handleTorrServerTracks(w http.ResponseWriter, r *http.Request) {
	user := s.userFromRequest(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var sig struct {
		AudioLang string `json:"audioLang"`
	}
	_ = datastar.ReadSignals(r, &sig)

	hash := r.URL.Query().Get("hash")
	idx, _ := strconv.Atoi(r.URL.Query().Get("index"))
	if idx <= 0 {
		idx = 1
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var audio, subs []torrserver.ProbeTrack
	if probe, err := s.torrServer.Client().Probe(ctx, hash, idx); err != nil {
		slog.Warn("tracks probe failed", "hash", logValue(hash), "index", idx, "error", logError(err))
	} else {
		for _, trk := range probe.Tracks {
			switch trk.Type {
			case "audio":
				audio = append(audio, trk)
			case "subtitle", "sub":
				subs = append(subs, trk)
			}
		}
	}

	selected := 0
	for _, trk := range audio {
		if sig.AudioLang != "" && strings.EqualFold(trk.Language, sig.AudioLang) {
			selected = trk.Index
			break
		}
	}

	sse := datastar.NewSSE(w, r)
	_ = sse.PatchElementTempl(views.TorrAudioPicker(audio, selected))
	_ = sse.PatchElementTempl(views.TorrSubtitleMenu(subs))
	_ = sse.MarshalAndPatchSignals(map[string]any{"audioTrack": selected})
}

// handleTorrServerPlaylist downloads an M3U playlist of signed links on this
// server: HLS through GStreamer when TorrServer has it, direct streams otherwise.
func (s *Server) handleTorrServerPlaylist(w http.ResponseWriter, r *http.Request) {
	if s.userFromRequest(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	hash := strings.ToLower(r.URL.Query().Get("hash"))
	if !isHexHash(hash) {
		http.Error(w, "Missing hash parameter", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	client := s.torrServer.Client()
	torrent, err := client.TorrentStats(ctx, hash)
	if err != nil {
		http.Error(w, "Failed to load torrent info", http.StatusBadGateway)
		return
	}
	echo, _ := client.Echo(ctx)

	// Direct links play the original files; HLS links GStreamer's conversion,
	// only when asked for.
	hls := r.URL.Query().Get("kind") == "hls"
	if hls && !echo.GSTAvailable {
		http.Error(w, "HLS playlists need GStreamer, which this TorrServer does not have.", http.StatusConflict)
		return
	}
	origin := s.linkOrigin(r)
	link := s.shareLink
	if hls {
		link = s.hlsShareLink
	}
	playlist := torrserver.GenerateM3UPlaylist(torrent.DisplayName(), torrent.FileStats, func(f torrserver.FileStat) string {
		return origin + link(hash, f)
	})

	filename := strings.NewReplacer("/", "_", `\`, "_", `"`, "").Replace(torrent.DisplayName())
	if hls {
		filename += " (HLS)"
	}
	w.Header().Set("Content-Type", "application/x-mpegurl; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store") // signed links expire
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.m3u8"`, filename))
	_, _ = w.Write([]byte(playlist)) // #nosec G705 -- a text/x-mpegurl download
}

// handleTorrServerQueue gives the player a torrent's videos, in order: the
// playlist menu and $_queue, which Next and the end of a file follow. A
// torrent it cannot load has an empty playlist.
func (s *Server) handleTorrServerQueue(w http.ResponseWriter, r *http.Request) {
	if s.userFromRequest(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	hash := strings.ToLower(r.URL.Query().Get("hash"))
	if !isHexHash(hash) {
		http.Error(w, "Missing hash parameter", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	var videos []torrserver.FileStat
	if torrent, err := s.torrServer.Client().TorrentStats(ctx, hash); err != nil {
		// hash is 40 hex digits by now; logValue still keeps line breaks out.
		slog.Warn("playlist: torrent stats failed", "hash", logValue(hash), "error", logValue(err.Error()))
	} else {
		videos = torrent.VideoFiles()
	}

	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(views.TorrPlaylist(videos)); err != nil {
		logSSEError(r, "patch playlist", err)
		return
	}
	if err := sse.MarshalAndPatchSignals(map[string]any{"_queue": views.PlayQueue(s.streamLinks(), hash, videos), "_queueHash": hash}); err != nil {
		logSSEError(r, "patch playlist signals", err)
	}
}

// handleTorrServerPlayerStats patches the stats row (and, with gst=1, the
// media info) under the player. With stream=true it keeps them current from
// the shared player topic, which also keeps TorrServer's GStreamer pipeline
// alive; stop=1 answers at once, so the player can end its previous stream.
func (s *Server) handleTorrServerPlayerStats(w http.ResponseWriter, r *http.Request) {
	user := s.userFromRequest(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if r.URL.Query().Get("stop") == "1" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	hash := r.URL.Query().Get("hash")
	if hash == "" {
		http.Error(w, "Missing hash parameter", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("stream") == "true" {
		s.streamPlayerStats(w, r, strings.ToLower(hash))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	client := s.torrServer.Client()
	if r.URL.Query().Get("gst") == "1" {
		_ = client.Heartbeat(ctx, hash)
	}
	stats, err := client.TorrentStats(ctx, hash)
	if err != nil {
		stats = nil
	}
	cacheSize, _ := client.CacheSize(ctx)

	sse := datastar.NewSSE(w, r)
	_ = sse.PatchElementTempl(views.TorrPlayerStats(stats, cacheSize))

	// Media info is only gathered while the player's info panel is open.
	if r.URL.Query().Get("info") != "1" {
		return
	}
	q := r.URL.Query()
	idx, _ := strconv.Atoi(q.Get("index"))
	if idx <= 0 {
		idx = 1
	}
	audio, _ := strconv.Atoi(q.Get("audio"))
	probe, err := client.Probe(ctx, hash, idx)
	if err != nil {
		probe = nil
	}
	var output *torrserver.HLSOutput
	if out, ok := s.hlsOutputs.load(hlsOutputKey{hash, idx, audio}); ok {
		output = &out
	}
	_ = sse.PatchElementTempl(views.TorrMediaInfo(probe, audio, output))
}

// hlsOutputKey identifies one GStreamer output: a file played with an audio track.
type hlsOutputKey struct {
	hash         string
	index, audio int
}

// parseHLSTrack reads the ?index=&audio= of a GStreamer stream: file 1 and
// audio track 0 when absent. It fails on anything but those numbers.
func parseHLSTrack(q url.Values) (index, audio int, ok bool) {
	index, audio = 1, 0
	var err error
	if v := q.Get("index"); v != "" {
		if index, err = strconv.Atoi(v); err != nil || index <= 0 {
			return 0, 0, false
		}
	}
	if v := q.Get("audio"); v != "" {
		if audio, err = strconv.Atoi(v); err != nil || audio < 0 {
			return 0, 0, false
		}
	}
	return index, audio, true
}

// maxHLSOutputs bounds hlsOutputCache: far more files than anyone plays
// between restarts, yet request parameters cannot grow it without limit.
const maxHLSOutputs = 256

// hlsOutputCache remembers the output of recently played GStreamer streams.
type hlsOutputCache struct {
	mu      sync.Mutex
	outputs map[hlsOutputKey]torrserver.HLSOutput
}

func (c *hlsOutputCache) store(key hlsOutputKey, out torrserver.HLSOutput) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.outputs == nil {
		c.outputs = make(map[hlsOutputKey]torrserver.HLSOutput)
	}
	if _, known := c.outputs[key]; !known && len(c.outputs) >= maxHLSOutputs {
		for k := range c.outputs { // evict an arbitrary entry
			delete(c.outputs, k)
			break
		}
	}
	c.outputs[key] = out
}

func (c *hlsOutputCache) load(key hlsOutputKey) (torrserver.HLSOutput, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out, ok := c.outputs[key]
	return out, ok
}

// proxiedHeaders are the upstream response headers a media player needs.
// Everything else, cookies above all, stays with TorrServer.
var proxiedHeaders = []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Content-Encoding", "Content-Disposition", "Cache-Control", "Last-Modified", "Etag"}

// copyProxiedHeaders forwards proxiedHeaders except skip, and sandboxes the
// response: TorrServer is user-configured, so a document it serves must not
// run with the app's origin.
func copyProxiedHeaders(dst, src http.Header, skip ...string) {
	for _, key := range proxiedHeaders {
		if slices.Contains(skip, key) {
			continue
		}
		for _, value := range src.Values(key) {
			dst.Add(key, value)
		}
	}
	dst.Set("Content-Security-Policy", "sandbox; default-src 'none'")
}

// isMediaPath reports whether a proxied TorrServer path serves media: the
// /stream play endpoint or GStreamer HLS under /gst/. The rest of the
// TorrServer API (settings, torrents, shutdown…) is never reachable here.
func isMediaPath(subpath string) bool {
	return subpath == "stream" || strings.HasPrefix(subpath, "stream/") || strings.HasPrefix(subpath, "gst/")
}

// handleTorrServerStreamProxy streams media to the signed-in player: the
// /stream play endpoint and GStreamer HLS under /gst/.
func (s *Server) handleTorrServerStreamProxy(w http.ResponseWriter, r *http.Request) {
	if s.userFromRequest(r) == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	subpath := strings.TrimPrefix(r.URL.Path, "/api/torrserver/stream/")
	if !isMediaPath(subpath) || strings.Contains(subpath, "..") || strings.Contains(subpath, ":") {
		http.NotFound(w, r)
		return
	}
	play := s.playRequest(r, s.userFromRequest(r).Name)
	rawQuery := r.URL.RawQuery
	if rest, ok := strings.CutPrefix(subpath, "gst/"); ok {
		hash, file, _ := strings.Cut(rest, "/")
		play.Hash, play.Kind = strings.ToLower(hash), playback.HLS
		play.File, play.Audio, play.Segment = hlsPosition(file, r.URL.Query())
	} else {
		// TorrServer's /stream also adds torrents (a magnet or a .torrent
		// address in link, save): only a file of a torrent it has may play.
		hash := strings.ToLower(r.URL.Query().Get("link"))
		file, err := strconv.Atoi(r.URL.Query().Get("index"))
		if !isHexHash(hash) || err != nil || file < 1 {
			http.Error(w, "Play a file by its torrent's info hash and file index.", http.StatusBadRequest)
			return
		}
		play.Hash, play.File = hash, file
		rawQuery = url.Values{"link": {hash}, "index": {strconv.Itoa(file)}}.Encode() + "&play"
	}
	s.proxyEngine(w, r, subpath, rawQuery, &play, func(line string) string {
		if strings.HasPrefix(line, "/gst/") {
			return "/api/torrserver/stream" + line
		}
		return strings.ReplaceAll(line, `URI="/gst/`, `URI="/api/torrserver/stream/gst/`)
	})
}

// proxyEngine forwards r to enginePath?rawQuery on the TorrServer in use,
// with its credentials. HLS playlists are rewritten line by line by rewrite,
// so the player's next requests come back through Moviestracker.
func (s *Server) proxyEngine(w http.ResponseWriter, r *http.Request, enginePath, rawQuery string, play *playback.Request, rewrite func(string) string) {
	targetURL := s.torrServer.ActiveURL() + "/" + enginePath
	if rawQuery != "" {
		targetURL += "?" + rawQuery
	}
	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, nil) // #nosec G704 -- the configured engine
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if user, password := s.torrServer.Credentials(); user != "" {
		proxyReq.SetBasicAuth(user, password)
	}

	isM3U8Path := strings.HasSuffix(enginePath, ".m3u8")
	// Forward important media streaming headers
	for _, h := range []string{"Range", "Accept", "Accept-Encoding", "User-Agent"} {
		if val := r.Header.Get(h); val != "" {
			if h == "Accept-Encoding" && isM3U8Path {
				continue // Request uncompressed playlist so we can inspect and rewrite paths
			}
			proxyReq.Header.Set(h, val)
		}
	}

	resp, err := http.DefaultClient.Do(proxyReq) // #nosec G704 -- the configured engine
	if err != nil {
		if r.Context().Err() != nil {
			return // the player hung up (closed, seeked); nothing failed
		}
		http.Error(w, "TorrServer did not answer.", http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	contentType := resp.Header.Get("Content-Type")
	isM3U8 := isM3U8Path || strings.Contains(contentType, "mpegurl") || strings.Contains(contentType, "m3u8")
	if !isM3U8 || rewrite == nil {
		copyProxiedHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		s.playing.Add(1) // Settings warn before a save interrupts this
		defer s.playing.Add(-1)
		if play == nil || play.Hash == "" || resp.StatusCode >= 300 {
			_, _ = io.Copy(w, resp.Body) // media chunks and segments pass straight through
			return
		}
		// The dashboard shows it while it plays, bytes counted as they pass.
		flow := s.plays.Open(*play)
		_, _ = io.Copy(w, sentCounter{resp.Body, flow})
		flow.Done()
		return
	}

	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		if gz, err := gzip.NewReader(resp.Body); err == nil {
			defer func() { _ = gz.Close() }()
			reader = gz
		}
	}
	scanner := bufio.NewScanner(reader)
	var buf bytes.Buffer
	for scanner.Scan() {
		buf.WriteString(rewrite(scanner.Text()) + "\n")
	}
	if err := scanner.Err(); err != nil {
		http.Error(w, "TorrServer sent an unreadable playlist.", http.StatusBadGateway)
		return
	}

	// Content-Length & Content-Encoding are dropped: the body is rewritten.
	copyProxiedHeaders(w.Header(), resp.Header, "Content-Length", "Content-Encoding")
	if resp.StatusCode == http.StatusOK && strings.HasSuffix(enginePath, "/master.m3u8") {
		hash := strings.TrimSuffix(strings.TrimPrefix(enginePath, "gst/"), "/master.m3u8")
		if query, err := url.ParseQuery(rawQuery); err == nil {
			if index, audio, ok := parseHLSTrack(query); ok {
				s.hlsOutputs.store(hlsOutputKey{hash, index, audio}, torrserver.ParseMasterPlaylist(buf.String()))
			}
		}
	}
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(buf.Bytes())
	if play != nil && play.Hash != "" && resp.StatusCode == http.StatusOK {
		s.plays.Observe(*play)
	}
}

// playRequest starts the description of a proxied player request.
func (s *Server) playRequest(r *http.Request, viewer string) playback.Request {
	return playback.Request{
		Client:  clientIP(r, s.trustedProxies),
		Viewer:  viewer,
		Kind:    playback.Direct,
		Offset:  rangeStart(r.Header.Get("Range")),
		Segment: -1,
	}
}

// rangeStart is the first byte of a "bytes=N-…" Range header (0 without one).
func rangeStart(header string) int64 {
	spec, ok := strings.CutPrefix(header, "bytes=")
	if !ok {
		return 0
	}
	first, _, _ := strings.Cut(spec, "-")
	n, _ := strconv.ParseInt(strings.TrimSpace(first), 10, 64)
	return max(n, 0)
}

// hlsPosition reads a GStreamer HLS request: the master playlist names the
// file and audio track, a segment its number (-1 for anything else).
func hlsPosition(path string, q url.Values) (file, audio, segment int) {
	segment = -1
	switch {
	case path == "master.m3u8":
		file, audio, _ = parseHLSTrack(q)
	case strings.HasPrefix(path, "seg/"):
		name := strings.TrimPrefix(path, "seg/")
		if n, err := strconv.Atoi(strings.TrimSuffix(name, ".m4s")); err == nil && n >= 0 {
			segment = n
		}
	}
	return file, audio, segment
}

// sentCounter counts a stream's bytes for the dashboard as the player reads them.
type sentCounter struct {
	r    io.Reader
	flow playback.Flow
}

func (c sentCounter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.flow.Sent(int64(n))
	}
	return n, err
}
