package web

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// maxTorrentList bounds a posted TorrServer list: hundreds of torrents with
// their files fit well within it.
const maxTorrentList = 2 << 20

// handleTorrents renders the torrents the user's browser read from their
// TorrServer (tsList), into #ts-torrents. The server itself never reaches
// the TorrServer.
func (a *app) handleTorrents(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentUser(r); !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var list struct {
		Torrents []torrserver.Torrent `json:"torrents"`
	}
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxTorrentList)).Decode(&list)
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		http.Error(w, "Request Entity Too Large", http.StatusRequestEntityTooLarge)
		return
	}
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.WebTorrents(list.Torrents)); err != nil {
		slog.Warn("patching torrents failed", "error", err)
	}
}
