package web

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	neturl "net/url"
	"slices"
	"time"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// maxTorrentList bounds a posted TorrServer list: hundreds of torrents with
// their files fit well within it. A list's torrents and each torrent's
// files are bounded too, as tiny ones would fit by the hundred thousand and
// each is rendered.
const (
	maxTorrentList     = 2 << 20
	maxTorrents        = 500
	maxFilesPerTorrent = 5000
)

// handleTorrents renders the torrents the user's browser read from their
// TorrServer (tsList), into #ts-torrents. The server itself never reaches
// the TorrServer.
func (a *app) handleTorrents(w http.ResponseWriter, r *http.Request) {
	torrents, ok := a.readTorrents(w, r)
	if !ok {
		return
	}
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.WebTorrents(torrents)); err != nil {
		slog.Warn("patching torrents failed", "error", err)
	}
}

// handleRecentTorrents renders home's row of the torrents last added to the
// TorrServer the browser listed.
func (a *app) handleRecentTorrents(w http.ResponseWriter, r *http.Request) {
	torrents, ok := a.readTorrents(w, r)
	if !ok {
		return
	}
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.RecentTorrents(torrents)); err != nil {
		slog.Warn("patching recent torrents failed", "error", err)
	}
}

// readTorrents is the signed-in visitor's torrent list the browser posted,
// within bounds; otherwise it answers the request and returns false.
func (a *app) readTorrents(w http.ResponseWriter, r *http.Request) ([]torrserver.Torrent, bool) {
	if _, ok := a.currentUser(r); !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return nil, false
	}
	var list struct {
		Torrents []torrserver.Torrent `json:"torrents"`
	}
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxTorrentList)).Decode(&list)
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		http.Error(w, "Request Entity Too Large", http.StatusRequestEntityTooLarge)
		return nil, false
	}
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return nil, false
	}
	if len(list.Torrents) > maxTorrents || slices.ContainsFunc(list.Torrents, func(t torrserver.Torrent) bool { return len(t.FileStats) > maxFilesPerTorrent }) {
		http.Error(w, "Request Entity Too Large", http.StatusRequestEntityTooLarge)
		return nil, false
	}
	return list.Torrents, true
}

// maxSSLStatus bounds a posted certificate status: a few names and paths.
const maxSSLStatus = 64 << 10

// handleCertificate renders the HTTPS certificate the user's browser read
// from their TorrServer at url (tsSSL), into #ts-https-details; the browser
// changes it itself.
func (a *app) handleCertificate(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentUser(r); !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var in struct {
		Status *torrserver.SSLStatus `json:"status"`
		URL    string                `json:"url"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSSLStatus)).Decode(&in); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.WebCertificate(in.Status, plainHTTP(in.URL), time.Now())); err != nil {
		slog.Warn("patching the TorrServer certificate failed", "error", err)
	}
}

// plainHTTP says whether the browser reaches the TorrServer at url over the
// network unencrypted, so an uploaded key would cross it in the clear.
func plainHTTP(url string) bool {
	u, err := neturl.Parse(url)
	if err != nil || u.Scheme != "http" {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return false
	}
	return true
}
