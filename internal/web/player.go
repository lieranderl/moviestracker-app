package web

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"

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
	if err != nil || !playerHash.MatchString(data.Hash) || data.Torrent == nil || data.Torrent.Hash != data.Hash || data.Index < 1 || (data.Kind != "direct" && data.Kind != "hls") {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return data, false
	}
	if len(data.Torrent.FileStats) > maxFilesPerTorrent || (data.Probe != nil && len(data.Probe.Tracks) > 256) {
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
	if err := sse.PatchElementTempl(views.WebPlayerControls(data.Hash, data.Index, data.Nonce, data.Torrent.VideoFiles(), audio, subs)); err != nil {
		slog.Warn("patching player failed", "error", err)
		return
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
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.TorrPlayerStats(data.Torrent, data.CacheSize)); err != nil {
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
