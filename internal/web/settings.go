package web

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/lieranderl/moviestracker-app/internal/handlers"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"
	"github.com/starfederation/datastar-go/datastar"
)

type settingsData struct {
	Section string            `json:"section"`
	URL     string            `json:"url"`
	Nonce   int               `json:"nonce"`
	Values  torrserver.Fields `json:"values"`
}

func (a *app) handleBrowserSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentUser(r); !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var data settingsData
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&data); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	s, ok := handlers.BrowserSettingsSection(data.Section, data.Values)
	if !ok || data.Values == nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.WebSettingsForm(s, data.URL, data.Nonce), datastar.WithModeReplace()); err != nil {
		slog.Warn("patching browser settings failed", "error", err)
	}
}

func (a *app) handleValidateBrowserSettings(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentUser(r); !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var data struct {
		Section string         `json:"section"`
		URL     string         `json:"url"`
		Nonce   int            `json:"nonce"`
		Values  map[string]any `json:"values"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10)).Decode(&data); err != nil || data.Values == nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	changes, err := handlers.BrowserSettingsChanges(r.Context(), data.Section, data.Values)
	problem := ""
	if err != nil {
		problem = err.Error()
	}
	w.Header().Set("Cache-Control", "no-store")
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.WebSettingsWrite(data.Section, data.URL, data.Nonce, changes, problem), datastar.WithModeReplace()); err != nil {
		slog.Warn("patching validated browser settings failed", "error", err)
	}
}
