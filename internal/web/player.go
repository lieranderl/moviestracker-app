package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/a-h/templ"
	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"
	"github.com/starfederation/datastar-go/datastar"
)

var playerHash = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)

type playerData struct {
	Hash      string                  `json:"hash"`
	Index     int                     `json:"index"`
	Kind      string                  `json:"kind"`
	Nonce     int                     `json:"nonce"`
	Torrent   *torrserver.Torrent     `json:"torrent"`
	Probe     *torrserver.ProbeResult `json:"probe"`
	Audio     int                     `json:"audio"`
	Master    string                  `json:"master"`
	CacheSize int64                   `json:"cacheSize"`
}

func (a *app) readPlayer(w http.ResponseWriter, r *http.Request) (playerData, bool) {
	if _, ok := a.currentUser(r); !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return playerData{}, false
	}
	var data playerData
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxTorrentList)).Decode(&data)
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		http.Error(w, "Request Entity Too Large", http.StatusRequestEntityTooLarge)
		return data, false
	}
	if err != nil || !playerHash.MatchString(data.Hash) || data.Torrent == nil || data.Torrent.Hash != data.Hash || data.Index < 1 || data.Audio < 0 || (data.Kind != "direct" && data.Kind != "hls") {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return data, false
	}
	if len(data.Master) > 65536 || len(data.Torrent.FileStats) > maxFilesPerTorrent || (data.Probe != nil && len(data.Probe.Tracks) > 256) {
		http.Error(w, "Request Entity Too Large", http.StatusRequestEntityTooLarge)
		return data, false
	}
	w.Header().Set("Cache-Control", "no-store")
	return data, true
}

// The player relays media data, never an address or login. Rendering makes
// no network request to the visitor's TorrServer.
func (a *app) handlePlayer(w http.ResponseWriter, r *http.Request) {
	data, ok := a.readPlayer(w, r)
	if !ok {
		return
	}
	sse := datastar.NewSSE(w, r)
	var audio, subs []torrserver.ProbeTrack
	if data.Probe != nil {
		for _, track := range data.Probe.Tracks {
			switch track.Type {
			case "audio":
				audio = append(audio, track)
			case "sub", "subtitle":
				subs = append(subs, track)
			}
		}
	}
	if err := sse.PatchElementTempl(views.WebPlayerControls(data.Hash, data.Index, data.Nonce, data.Torrent.VideoFiles()), datastar.WithModeReplace()); err != nil {
		slog.Warn("patching player failed", "error", err)
		return
	}
	for _, component := range []templ.Component{
		views.TorrAudioPicker(audio, 0), views.TorrSubtitleMenu(subs), views.TorrPlaylist(data.Torrent.VideoFiles(), true), playerMediaInfo(data),
	} {
		if err := sse.PatchElementTempl(component); err != nil {
			slog.Warn("patching player controls failed", "error", err)
			return
		}
	}
	if err := sse.PatchElementTempl(views.TorrPlayerStats(data.Torrent, data.CacheSize)); err != nil {
		slog.Warn("patching player stats failed", "error", err)
	}
}

func (a *app) handlePlayerStats(w http.ResponseWriter, r *http.Request) {
	data, ok := a.readPlayer(w, r)
	if !ok {
		return
	}
	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(playerMediaInfo(data)); err != nil {
		slog.Warn("patching media info failed", "error", err)
		return
	}
	if err := sse.PatchElementTempl(views.TorrPlayerStats(data.Torrent, data.CacheSize)); err != nil {
		slog.Warn("patching player stats failed", "error", err)
	}
}

func (a *app) handlePlayerFiles(w http.ResponseWriter, r *http.Request) {
	data, ok := a.readPlayer(w, r)
	if !ok {
		return
	}
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.WebTorrent(data.Torrent)); err != nil {
		slog.Warn("patching torrent files failed", "error", err)
	}
}

// handleBrowserProbe renders the shared analysis from a browser-only request.
func (a *app) handleBrowserProbe(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentUser(r); !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var data struct {
		Hash  string                  `json:"hash"`
		Index int                     `json:"index"`
		Probe *torrserver.ProbeResult `json:"probe"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxTorrentList)).Decode(&data); err != nil || !playerHash.MatchString(data.Hash) || data.Index < 1 {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if data.Probe != nil && len(data.Probe.Tracks) > 256 {
		http.Error(w, "Request Entity Too Large", http.StatusRequestEntityTooLarge)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	component := views.TorrServerProbeFragment(data.Probe)
	if data.Probe == nil {
		component = views.TorrServerProbeError(i18n.T(r.Context(), "TorrServer could not load playback information. Check the connection or login, then retry."))
	}
	if err := datastar.NewSSE(w, r).PatchElementTempl(component); err != nil {
		slog.Warn("patching media analysis failed", "error", err)
	}
}

// handleBrowserPlaylist reuses the local app's exporter. The relayed address
// is used only to format links; this handler makes no TorrServer requests.
func (a *app) handleBrowserPlaylist(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentUser(r); !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var data struct {
		URL     string              `json:"url"`
		Kind    string              `json:"kind"`
		Torrent *torrserver.Torrent `json:"torrent"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxTorrentList)).Decode(&data); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	base, err := url.Parse(data.URL)
	validURL := err == nil && base.Host != "" && (base.Scheme == "http" || base.Scheme == "https") && base.User == nil && base.RawQuery == "" && base.Fragment == "" && len(data.URL) <= 300
	validTorrent := data.Torrent != nil && playerHash.MatchString(data.Torrent.Hash)
	if !validURL || !validTorrent || (data.Kind != "direct" && data.Kind != "hls") {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if len(data.Torrent.FileStats) > maxFilesPerTorrent {
		http.Error(w, "Request Entity Too Large", http.StatusRequestEntityTooLarge)
		return
	}
	origin := strings.TrimRight(base.String(), "/") + "/"
	playlist := torrserver.GenerateM3UPlaylist(data.Torrent.DisplayName(), data.Torrent.FileStats, func(f torrserver.FileStat) string {
		if data.Kind == "hls" {
			return fmt.Sprintf("%sgst/%s/master.m3u8?index=%d", origin, data.Torrent.Hash, f.ID)
		}
		return fmt.Sprintf("%sstream?link=%s&index=%d&play", origin, data.Torrent.Hash, f.ID)
	})
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, playlist)
}

func playerMediaInfo(data playerData) templ.Component {
	var output *torrserver.HLSOutput
	if data.Master != "" {
		parsed := torrserver.ParseMasterPlaylist(data.Master)
		output = &parsed
	}
	return views.TorrMediaInfo(data.Probe, data.Audio, output)
}
