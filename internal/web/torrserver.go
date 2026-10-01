package web

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/moviestracker-app/internal/handlers"
	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// handleTorrServerPage lists the user's TorrServers; their browser does the
// rest (static/torrserver.js).
func (a *app) handleTorrServerPage(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	servers, err := a.cfg.Store.TorrServers(ctx, user.ID)
	if err != nil {
		slog.Warn("reading TorrServers failed", "error", handlers.LogError(err))
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.WebTorrServerPage(a.catalogUser(r), servers, handlers.BrowserSettingsSections()).Render(r.Context(), w); err != nil {
		slog.Warn("render failed", "page", "torrserver", "error", err)
	}
}

func (a *app) handleTorrServerSelector(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	servers, err := a.cfg.Store.TorrServers(ctx, user.ID)
	if err != nil {
		slog.Warn("reading TorrServers failed", "error", handlers.LogError(err))
		w.Header().Set("Cache-Control", "no-store")
		if err := datastar.NewSSE(w, r).PatchElementTempl(views.WebSourceSelectorError()); err != nil {
			slog.Warn("patching the TorrServer selector failed", "error", err)
		}
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.WebSourceSelector(servers)); err != nil {
		slog.Warn("patching the TorrServer selector failed", "error", err)
	}
}

// torrServerSignals are the add form's fields; its login is never sent.
type torrServerSignals struct {
	Name string `json:"tsName"`
	URL  string `json:"tsUrl"`
}

// handleAddTorrServer keeps a TorrServer's address, picks it in the
// browser, and clears the form; an address that is not one is explained.
func (a *app) handleAddTorrServer(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var form torrServerSignals
	if err := datastar.ReadSignals(r, &form); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	sse := datastar.NewSSE(w, r)
	saved, err := a.cfg.Store.SaveTorrServer(ctx, user.ID, store.TorrServer{Name: form.Name, URL: form.URL})
	if err != nil {
		msg := i18n.T(r.Context(), "Enter TorrServer's web address, such as http://localhost:8090 or https://nas.example:8091.")
		if err := sse.MarshalAndPatchSignals(map[string]string{"tsFormError": msg}); err != nil {
			slog.Warn("patching the TorrServer form failed", "error", err)
		}
		return
	}
	a.patchTorrServers(ctx, sse, user.ID)
	if err := sse.MarshalAndPatchSignals(map[string]any{
		"tsSelected": saved.URL, "tsAddOpen": false, "tsName": "", "tsUrl": "", "tsUser": "", "tsPass": "", "tsFormError": "",
	}); err != nil {
		slog.Warn("patching the TorrServer form failed", "error", err)
	}
}

// handleRemoveTorrServer forgets one of the user's TorrServers. Once it is
// gone, the browser forgets its login ($tsForget) and, if it was the one
// picked ($tsSelected), picks the first left, if any.
func (a *app) handleRemoveTorrServer(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var picked struct {
		Selected string `json:"tsSelected"`
	}
	if err := datastar.ReadSignals(r, &picked); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	id := r.PathValue("id")
	servers, err := a.cfg.Store.TorrServers(ctx, user.ID)
	if err == nil {
		err = a.cfg.Store.RemoveTorrServer(ctx, user.ID, id)
	}
	if err != nil {
		slog.Warn("removing a TorrServer failed", "error", handlers.LogError(err))
		http.Error(w, i18n.T(r.Context(), "The TorrServer could not be removed. Please try again."), http.StatusServiceUnavailable)
		return
	}
	signals := map[string]string{}
	var left []store.TorrServer
	for _, s := range servers {
		if s.ID == id {
			signals["tsForget"] = s.URL
			if s.URL == picked.Selected {
				signals["tsSelected"] = ""
			}
		} else {
			left = append(left, s)
		}
	}
	if _, repick := signals["tsSelected"]; repick && len(left) > 0 {
		signals["tsSelected"] = left[0].URL
	}
	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(views.WebTorrServers(left)); err != nil {
		slog.Warn("patching TorrServers failed", "error", err)
	}
	if len(signals) > 0 {
		if err := sse.MarshalAndPatchSignals(signals); err != nil {
			slog.Warn("patching the TorrServer pick failed", "error", err)
		}
	}
}

// patchTorrServers sends the user's TorrServers as they are kept.
func (a *app) patchTorrServers(ctx context.Context, sse *datastar.ServerSentEventGenerator, uid string) {
	servers, err := a.cfg.Store.TorrServers(ctx, uid)
	if err != nil {
		slog.Warn("reading TorrServers failed", "error", handlers.LogError(err))
		return
	}
	if err := sse.PatchElementTempl(views.WebTorrServers(servers)); err != nil {
		slog.Warn("patching TorrServers failed", "error", err)
	}
}
