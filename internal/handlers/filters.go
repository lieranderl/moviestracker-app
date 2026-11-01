package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// ReleaseFilterPrefs remembers how each user last searched a title's
// releases, for both apps.
type ReleaseFilterPrefs struct {
	store store.ReleaseFilterStore
	user  func(*http.Request) (uid string, ok bool)
}

// NewReleaseFilterPrefs keeps release filters in s for the users user names.
func NewReleaseFilterPrefs(s store.ReleaseFilterStore, user func(*http.Request) (string, bool)) *ReleaseFilterPrefs {
	return &ReleaseFilterPrefs{store: s, user: user}
}

// Register adds the release filters' route to mux.
func (p *ReleaseFilterPrefs) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/preferences/release-filters", p.handleSave)
}

// releaseFilterSignals are the Sources section's filters as Datastar sends
// them: $qual holds each quality checkbox's value, "" while unticked.
type releaseFilterSignals struct {
	Sort           string   `json:"sort"`
	Qual           []string `json:"qual"`
	HDR            bool     `json:"hdr"`
	HideRecordings bool     `json:"hideRecordings"`
}

func (p *ReleaseFilterPrefs) handleSave(w http.ResponseWriter, r *http.Request) {
	uid, ok := p.user(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var signals releaseFilterSignals
	if err := datastar.ReadSignals(r, &signals); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	filters := store.ReleaseFilters{Sort: signals.Sort, HDR: signals.HDR, HideRecordings: signals.HideRecordings}
	for _, q := range signals.Qual {
		if q == "" {
			continue
		}
		n, err := strconv.Atoi(q)
		if err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		filters.Qualities = append(filters.Qualities, n)
	}
	if store.CheckReleaseFilters(filters) != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	if err := p.store.SaveReleaseFilters(ctx, uid, filters); err != nil {
		slog.Warn("saving release filters failed", "error", logError(err))
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// withFilters is r with its user's release filters in its context, for the
// title page to start with; r as it is for a nil p or when none are read.
func (p *ReleaseFilterPrefs) withFilters(r *http.Request) *http.Request {
	if p == nil {
		return r
	}
	uid, ok := p.user(r)
	if !ok {
		return r
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	f, err := p.store.ReleaseFilters(ctx, uid)
	if err != nil {
		slog.Warn("reading release filters failed", "error", logError(err))
		return r
	}
	return r.WithContext(views.WithReleaseFilters(r.Context(), views.ReleaseFilters(f)))
}
