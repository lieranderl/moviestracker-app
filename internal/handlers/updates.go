package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/a-h/templ"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/update"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// available is the newer release, if there is one and checks are on.
func (s *Server) available() (update.Release, bool) {
	if s.updates == nil {
		return update.Release{}, false
	}
	return s.updates.Available()
}

// updateNotice hands pages the newer release, for the navbar to announce to
// administrators.
func (s *Server) updateNotice(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rel, ok := s.available(); ok {
			r = r.WithContext(views.WithRelease(r.Context(), views.Release{Version: rel.Version}))
		}
		next.ServeHTTP(w, r)
	})
}

// updatesView is Settings → Updates.
func (s *Server) updatesView() views.UpdatesView {
	v := views.UpdatesView{Current: s.version, Checking: !s.store.State().Updates.Off}
	if s.updates == nil {
		return v
	}
	v.Docker = s.updates.Platform() == update.Docker
	if rel, ok := s.updates.Available(); ok {
		v.Available, v.Newer, v.Notes, v.Download = true, views.Release{Version: rel.Version}, rel.Notes, rel.Download
	}
	return v
}

// handleUpdatesPage serves Settings → Updates.
func (s *Server) handleUpdatesPage(w http.ResponseWriter, r *http.Request) {
	user := s.adminPage(w, r)
	if user == nil {
		return
	}
	templ.Handler(views.UpdatesPage(user, s.updatesView())).ServeHTTP(w, r)
}

// handleSaveUpdates turns the daily release check on or off. Turned on, it
// asks GitHub at once, unless it did in the last day.
func (s *Server) handleSaveUpdates(w http.ResponseWriter, r *http.Request) {
	var sig struct {
		Check bool `json:"updatesCheck"`
	}
	if !s.sourceAction(w, r, "", &sig) {
		return
	}
	st := succeeded("Moviestracker no longer looks for new releases.")
	if err := s.store.Update(func(state *config.State) error {
		state.Updates.Off = !sig.Check
		return nil
	}); err != nil {
		slog.Error("save the release check", "error", err)
		st = failed(saveFailed)
	} else if sig.Check {
		st = succeeded("Moviestracker looks for new releases once a day.")
		if s.updates != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
			s.updates.RefreshIfDue(ctx)
			cancel()
		}
	}
	v := s.updatesView()
	patchSource(w, r, views.UpdatesCheck(v, st), map[string]any{"updatesCheck": v.Checking})
}

// handleUpdateForApps tells the menu bar and tray apps about a newer
// release: JSON, or 204 No Content when there is none. They run on this
// machine and have no session, so nobody else may ask.
func (s *Server) handleUpdateForApps(w http.ResponseWriter, r *http.Request) {
	if !fromThisMachine(r) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	rel, ok := s.available()
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(struct {
		Version  string `json:"version"`
		Notes    string `json:"notes"`
		Download string `json:"download,omitempty"`
	}{rel.Version, rel.Notes, rel.Download})
}
