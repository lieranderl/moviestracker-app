package handlers

import (
	"context"
	"errors"
	"github.com/a-h/templ"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"sync"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// DiscoveryPresets serves account-owned saved filters through the shared catalog.
type DiscoveryPresets struct {
	store   store.DiscoveryPresetStore
	user    func(*http.Request) (string, bool)
	catalog *Catalog
	guard   *sync.RWMutex
}

// NewDiscoveryPresets uses each app's storage and identity adapters.
func NewDiscoveryPresets(s store.DiscoveryPresetStore, user func(*http.Request) (string, bool), catalog *Catalog) *DiscoveryPresets {
	return &DiscoveryPresets{store: s, user: user, catalog: catalog}
}

// KeepDiscoveryPresets enables the shared Discover page's saved filters.
func (c *Catalog) KeepDiscoveryPresets(p *DiscoveryPresets) { c.presets = p }

// Register adds the preset routes before the app starts serving requests.
func (p *DiscoveryPresets) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/discovery-presets", p.guarded(p.handleCreate))
	mux.HandleFunc("GET /discover/presets/{id}", p.guarded(p.handleApply))
	mux.HandleFunc("POST /api/discovery-presets/{id}/rename", p.guarded(p.handleRename))
	mux.HandleFunc("DELETE /api/discovery-presets/{id}", p.guarded(p.handleDelete))
}

// guarded fences local preset requests against account deletion and session revocation.
func (p *DiscoveryPresets) guarded(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if p.guard != nil {
			p.guard.RLock()
			defer p.guard.RUnlock()
		}
		next(w, r)
	}
}

func discoveryFilters(q tmdb.DiscoverQuery) store.DiscoveryFilters {
	return store.DiscoveryFilters{Kind: q.MediaType, Genre: q.Genre, Year: q.Year, MinRating: q.MinRating, Sort: q.Sort, MaxRuntime: q.MaxRuntime, Language: q.Language}
}

func discoverQuery(f store.DiscoveryFilters) tmdb.DiscoverQuery {
	return tmdb.DiscoverQuery{MediaType: f.Kind, Genre: f.Genre, Year: f.Year, MinRating: f.MinRating, Sort: f.Sort, MaxRuntime: f.MaxRuntime, Language: f.Language}
}

func (p *DiscoveryPresets) target(w http.ResponseWriter, r *http.Request) (string, bool) {
	uid, ok := p.user(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}
	return uid, ok
}

func (p *DiscoveryPresets) handleCreate(w http.ResponseWriter, r *http.Request) {
	uid, ok := p.target(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var signals struct {
		Name string `json:"presetName"`
	}
	if err := datastar.ReadSignals(r, &signals); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	ctx, cancel := p.catalog.detailsContext(r)
	defer cancel()
	v, err := p.catalog.discoverView(ctx, r)
	if err != nil {
		p.failed(w, r, err)
		return
	}
	filters := discoveryFilters(v.Query)
	if _, err := p.store.CreateDiscoveryPreset(ctx, uid, signals.Name, filters); err != nil {
		p.failed(w, r, err)
		return
	}
	p.patch(w, r, uid, v.FilterParams(), i18n.T(r.Context(), "Filters saved."), false)
}

func (p *DiscoveryPresets) patch(w http.ResponseWriter, r *http.Request, uid, query, message string, focusSave bool) {
	v := p.load(r, uid, query)
	v.Message = message
	w.Header().Set("Cache-Control", "no-store")
	sse := datastar.NewSSE(w, r)
	if err := sse.MarshalAndPatchSignals(map[string]any{"_presetNewName": "", "_presetManageOpen": false}); err != nil {
		return
	}
	if err := sse.PatchElementTempl(views.DiscoveryPresets(v)); err != nil {
		slog.Warn("patch discovery presets failed", "error", logError(err))
	}
	if !focusSave && r.PathValue("id") != "" {
		if err := sse.MarshalAndPatchSignals(map[string]any{"_presetFocusManage": r.PathValue("id")}); err != nil {
			slog.Warn("focus preset control failed", "error", logError(err))
		}
	}
	if focusSave {
		if err := sse.MarshalAndPatchSignals(map[string]any{"_presetFocusSave": true}); err != nil {
			slog.Warn("focus preset form failed", "error", logError(err))
		}
	}
}

// pageState revalidates the session and reads its presets in one account lifetime.
func (p *DiscoveryPresets) pageState(r *http.Request, query string) *views.DiscoveryPresetsState {
	if p.guard != nil {
		p.guard.RLock()
		defer p.guard.RUnlock()
	}
	uid, ok := p.user(r)
	if !ok {
		return nil
	}
	state := p.load(r, uid, query)
	state.Adjusted = r.URL.Query().Get("presetAdjusted") == "1"
	return &state
}

func (p *DiscoveryPresets) load(r *http.Request, uid, query string) views.DiscoveryPresetsState {
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	items, err := p.store.DiscoveryPresets(ctx, uid)
	if err != nil {
		slog.Warn("read discovery presets failed", "error", logError(err))
	}
	return views.DiscoveryPresetsState{Items: items, Query: query, Failed: err != nil}
}

// presetReturnQuery keeps only the current form's durable fields in a patch.
func presetReturnQuery(r *http.Request) string {
	query := url.Values{}
	for _, key := range []string{"type", "genre", "year", "rating", "sort", "runtime", "lang"} {
		if value := r.URL.Query().Get(key); value != "" {
			query.Set(key, value)
		}
	}
	return query.Encode()
}

func (p *DiscoveryPresets) handleRename(w http.ResponseWriter, r *http.Request) {
	uid, ok := p.target(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var signals struct {
		Name string `json:"presetName"`
	}
	if err := datastar.ReadSignals(r, &signals); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	if err := p.store.RenameDiscoveryPreset(ctx, uid, r.PathValue("id"), signals.Name); err != nil {
		p.failed(w, r, err)
		return
	}
	p.patch(w, r, uid, presetReturnQuery(r), i18n.T(r.Context(), "Filter name updated."), false)
}

func (p *DiscoveryPresets) handleDelete(w http.ResponseWriter, r *http.Request) {
	uid, ok := p.target(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	if err := p.store.DeleteDiscoveryPreset(ctx, uid, r.PathValue("id")); err != nil {
		p.failed(w, r, err)
		return
	}
	p.patch(w, r, uid, presetReturnQuery(r), i18n.T(r.Context(), "Saved filter deleted."), true)
}

func (p *DiscoveryPresets) failed(w http.ResponseWriter, r *http.Request, actionErr error) {
	if errors.Is(actionErr, store.ErrAccountDeleted) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	message := i18n.T(r.Context(), "Your filters could not be saved. Please try again.")
	switch {
	case errors.Is(actionErr, store.ErrPresetLimit):
		message = i18n.Tf(r.Context(), "You can save up to %d filter presets.", store.MaxDiscoveryPresets)
	case errors.Is(actionErr, store.ErrInvalidPreset):
		message = i18n.T(r.Context(), "Use a name from 1 to 60 characters.")
	case errors.Is(actionErr, store.ErrPresetNotFound):
		message = i18n.T(r.Context(), "This saved filter no longer exists.")
	default:
		slog.Warn("saving discovery filters failed", "error", logError(actionErr))
	}
	w.Header().Set("Cache-Control", "no-store")
	sse := datastar.NewSSE(w, r)
	for _, id := range []string{"discovery-preset-status", "discovery-preset-dialog-status"} {
		if err := sse.PatchElementTempl(views.DiscoveryPresetNotice(id, message, true)); err != nil {
			slog.Warn("patch preset error failed", "error", logError(err))
			return
		}
	}
}

func (p *DiscoveryPresets) handleApply(w http.ResponseWriter, r *http.Request) {
	uid, ok := p.target(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	items, err := p.store.DiscoveryPresets(ctx, uid)
	if err != nil {
		p.applyUnavailable(w, r)
		return
	}
	i := slices.IndexFunc(items, func(v store.DiscoveryPreset) bool { return v.ID == r.PathValue("id") })
	if i < 0 {
		http.NotFound(w, r)
		return
	}
	filters := items[i].Filters
	q, _ := url.ParseQuery(views.DiscoverView{Query: discoverQuery(filters)}.FilterParams())
	request := r.Clone(r.Context())
	request.URL = &url.URL{Path: "/discover", RawQuery: q.Encode()}
	lookup, stop := p.catalog.detailsContext(request)
	defer stop()
	v, err := p.catalog.discoverView(lookup, request)
	if err != nil {
		p.applyUnavailable(w, r)
		return
	}
	query, _ := url.ParseQuery(v.FilterParams())
	if discoveryFilters(v.Query) != filters {
		query.Set("presetAdjusted", "1")
	}
	http.Redirect(w, r, "/discover?"+query.Encode(), http.StatusSeeOther)
}

func (p *DiscoveryPresets) applyUnavailable(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	templ.Handler(views.DiscoveryPresetUnavailable(p.catalog.userFromRequest(r))).ServeHTTP(w, r)
}
