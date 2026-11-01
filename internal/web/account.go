package web

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/moviestracker-app/internal/handlers"
	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// handleAccountPage is Your data: the download and the account's deletion.
func (a *app) handleAccountPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentUser(r); !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.AccountPage(a.catalogUser(r)).Render(r.Context(), w); err != nil {
		slog.Warn("render failed", "page", "account", "error", err)
	}
}

// handleAccountExport downloads everything kept for the user, as JSON.
func (a *app) handleAccountExport(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(r)
	if !ok {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	account, err := a.cfg.Store.ExportAccount(ctx, user.ID)
	if err != nil {
		slog.Warn("exporting an account failed", "error", handlers.LogError(err))
		http.Error(w, i18n.T(r.Context(), "Your data could not be read right now. Please try again in a moment."), http.StatusServiceUnavailable)
		return
	}
	body, err := json.MarshalIndent(account, "", "  ")
	if err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="moviestracker-data-%s.json"`, a.now().Format("2006-01-02")))
	_, _ = w.Write(body)
}

// handleAccountDelete deletes everything kept for the user once they typed
// the confirmation word ($deleteConfirm), then signs them out.
func (a *app) handleAccountDelete(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var signals struct {
		DeleteConfirm string `json:"deleteConfirm"`
	}
	_ = datastar.ReadSignals(r, &signals)
	typed := strings.TrimSpace(signals.DeleteConfirm)
	if !strings.EqualFold(typed, i18n.T(r.Context(), "delete")) && !strings.EqualFold(typed, "delete") {
		a.patchDeleteProblem(w, r, i18n.T(r.Context(), "Type the confirmation word to delete your account."))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	if err := a.cfg.Store.DeleteAccount(ctx, user.ID); err != nil {
		slog.Warn("deleting an account failed", "error", handlers.LogError(err))
		a.patchDeleteProblem(w, r, i18n.T(r.Context(), "Your account could not be deleted completely. Please try again: what is left is deleted then."))
		return
	}
	slog.Info("an account was deleted")
	http.SetCookie(w, a.cookie(sessionCookie, "", "/", -1))
	handlers.Navigate(w, r, "/")
}

func (a *app) patchDeleteProblem(w http.ResponseWriter, r *http.Request, msg string) {
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.AccountDeleteProblem(msg)); err != nil {
		slog.Warn("patching the deletion problem failed", "error", err)
	}
}
