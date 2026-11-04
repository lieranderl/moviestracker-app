package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// EpisodeMappings confirms an account's exact torrent-file association.
type EpisodeMappings struct {
	store   store.EpisodeMappingStore
	user    func(*http.Request) (string, bool)
	catalog *Catalog
	torrent func(context.Context, string) (torrserver.Torrent, bool)
	guard   *sync.RWMutex
}

// NewEpisodeMappings connects each transport's account, metadata and persistence.
func NewEpisodeMappings(s store.EpisodeMappingStore, user func(*http.Request) (string, bool), catalog *Catalog, torrent func(context.Context, string) (torrserver.Torrent, bool)) *EpisodeMappings {
	return &EpisodeMappings{store: s, user: user, catalog: catalog, torrent: torrent}
}

// Register adds the authenticated mapping endpoints.
func (m *EpisodeMappings) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/episodes/mapping", m.handleGet)
	mux.HandleFunc("POST /api/episodes/mapping", m.handleSave)
	mux.HandleFunc("DELETE /api/episodes/mapping", m.handleClear)
}

// errMappingSignedOut reports an account that ended while its request ran.
var errMappingSignedOut = errors.New("episode mapping: account signed out")

// write changes the account's mappings while no local account is created or
// deleted, rechecking the session under that guard. Metadata lookups stay
// outside it, so a slow provider never holds up account changes.
func (m *EpisodeMappings) write(r *http.Request, uid string, change func() error) error {
	if m.guard != nil {
		m.guard.RLock()
		defer m.guard.RUnlock()
	}
	if now, ok := m.user(r); !ok || now != uid {
		return errMappingSignedOut
	}
	return change()
}

func (m *EpisodeMappings) target(w http.ResponseWriter, r *http.Request) (string, views.EpisodeMappingView, url.Values, bool) {
	w.Header().Set("Cache-Control", "no-store")
	uid, ok := m.user(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return "", views.EpisodeMappingView{}, nil, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var err error
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/form-data") {
		err = r.ParseMultipartForm(maxRequestBodyBytes)
		if r.MultipartForm != nil {
			defer func() { _ = r.MultipartForm.RemoveAll() }()
		}
	} else {
		err = r.ParseForm()
	}
	if err != nil {
		var tooBig *http.MaxBytesError
		status := http.StatusBadRequest
		if errors.As(err, &tooBig) {
			status = http.StatusRequestEntityTooLarge
		}
		http.Error(w, http.StatusText(status), status)
		return "", views.EpisodeMappingView{}, nil, false
	}
	p := r.Form
	index, e1 := strconv.Atoi(p.Get("index"))
	tv, e2 := strconv.Atoi(p.Get("tv"))
	key, e3 := strconv.ParseInt(p.Get("key"), 10, 53)
	file, hash := p.Get("file"), strings.ToLower(p.Get("hash"))
	if e1 != nil || e2 != nil || e3 != nil || index < 1 || tv < 1 || key < 1 || !infoHash.MatchString(hash) || file == "" || len(file) > 4096 || !torrserver.IsVideoFile(file) {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return "", views.EpisodeMappingView{}, nil, false
	}
	v := views.EpisodeMappingView{
		Context:        views.EpisodeMappingContext{Hash: hash, Index: index, File: file, TMDBID: tv, Request: key},
		SelectedSeason: -1, Editing: p.Get("editing") == "1" || r.Method == http.MethodPost,
	}
	return uid, v, p, true
}

func (m *EpisodeMappings) load(r *http.Request, uid string, v views.EpisodeMappingView, p url.Values) views.EpisodeMappingView {
	ctx, cancel := m.catalog.detailsContext(r)
	defer cancel()
	if m.torrent != nil {
		torrent, found := m.torrent(ctx, v.Context.Hash)
		kind, tv := torrent.TMDB()
		fileOK := false
		for _, file := range torrent.FileStats {
			fileOK = fileOK || (file.ID == v.Context.Index && file.Path == v.Context.File)
		}
		if !found || !strings.EqualFold(torrent.Hash, v.Context.Hash) || kind != "tv" || tv != v.Context.TMDBID || !fileOK {
			v.Problem = i18n.T(r.Context(), "File details are unavailable. Refresh TorrServer and try again.")
			v.Blocked = true
			return v
		}
	}
	kept, found, err := m.store.EpisodeMapping(ctx, uid, v.Context.Hash, v.Context.Index)
	if err != nil {
		slog.Warn("reading episode confirmation failed", "error", logError(err))
		v.Problem = i18n.T(r.Context(), "Episode confirmations could not be loaded. Please try again.")
		v.Blocked = true
		return v
	}
	if found {
		if kept.TMDBID == v.Context.TMDBID && kept.File == v.Context.File {
			v.Saved = &kept
		} else {
			v.Stale = true
			v.Message = i18n.T(r.Context(), "File details changed. Confirm its episodes again.")
		}
	}
	clients := m.catalog.clients()
	if clients == nil || clients.Details == nil {
		v.Problem = i18n.T(r.Context(), "Episode details could not be loaded. Please try again.")
		v.Blocked = true
		return v
	}
	v.Series, err = clients.Details.TV(ctx, v.Context.TMDBID)
	if err != nil || v.Series == nil || v.Series.ID != v.Context.TMDBID {
		v.Problem = i18n.T(r.Context(), "Episode details could not be loaded. Please try again.")
		v.Blocked = true
		return v
	}
	if raw := p.Get("season"); raw != "" {
		v.SelectedSeason, err = strconv.Atoi(raw)
		if err != nil || v.SelectedSeason < 0 {
			v.SelectedSeason = -1
			v.Problem = i18n.T(r.Context(), "Choose a season from this series.")
			return v
		}
	} else if r.Method == http.MethodGet {
		if v.Saved != nil {
			v.SelectedSeason, v.First, v.Last = v.Saved.Season, v.Saved.First, v.Saved.Last
		} else if !found {
			season, first, last, exact := torrserver.ExactEpisodes(v.Context.File)
			if exact {
				v.SelectedSeason, v.First, v.Last, v.FromFilename = season, first, last, true
			}
		}
	}
	if v.SelectedSeason >= 0 {
		known := false
		for _, season := range v.Series.Seasons {
			known = known || season.Number == v.SelectedSeason
		}
		if !known {
			// Only a season the form submitted is a mistake to point out; one
			// from the file name or an old confirmation is left to choose.
			if p.Get("season") != "" {
				v.Problem = i18n.T(r.Context(), "Choose a season from this series.")
			}
			v.SelectedSeason, v.First, v.Last, v.FromFilename = -1, 0, 0, false
			return v
		}
	}
	// The read-only view shows the saved or filename range without its episodes.
	if v.SelectedSeason >= 0 && v.Editing {
		v.Season, err = clients.Details.Season(ctx, v.Context.TMDBID, v.SelectedSeason)
		if err != nil || v.Season == nil || v.Season.Number != v.SelectedSeason || v.Season.ShowID != v.Context.TMDBID {
			v.Problem = i18n.T(r.Context(), "Episode details could not be loaded. Please try again.")
			v.Blocked = true
			return v
		}
	}
	if r.Method == http.MethodPost {
		v.First, _ = strconv.Atoi(p.Get("first"))
		v.Last, _ = strconv.Atoi(p.Get("last"))
	} else if v.Saved != nil && v.SelectedSeason == v.Saved.Season {
		v.First, v.Last = v.Saved.First, v.Saved.Last
	}
	return v
}

func (m *EpisodeMappings) handleGet(w http.ResponseWriter, r *http.Request) {
	uid, v, p, ok := m.target(w, r)
	if !ok {
		return
	}
	m.render(w, r, m.load(r, uid, v, p))
}

func (m *EpisodeMappings) handleSave(w http.ResponseWriter, r *http.Request) {
	uid, v, p, ok := m.target(w, r)
	if !ok {
		return
	}
	v = m.load(r, uid, v, p)
	v.Focus = true
	if v.Blocked || v.Problem != "" {
		m.render(w, r, v)
		return
	}
	if !validMappingRange(v) {
		v.Problem = i18n.T(r.Context(), "Choose a valid episode range in this season.")
		m.render(w, r, v)
		return
	}
	kept := store.EpisodeMapping{
		Hash: v.Context.Hash, Index: v.Context.Index, File: v.Context.File,
		TMDBID: v.Context.TMDBID, Season: v.SelectedSeason, First: v.First, Last: v.Last,
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	if err := m.write(r, uid, func() error { return m.store.SaveEpisodeMapping(ctx, uid, kept) }); err != nil {
		slog.Warn("saving episode confirmation failed", "error", logError(err))
		v.Problem = i18n.T(r.Context(), "Episode confirmation could not be saved. Please try again.")
	} else {
		v.Saved, v.Editing, v.Stale, v.FromFilename = &kept, false, false, false
		v.Message = i18n.T(r.Context(), "Episodes confirmed for this file.")
	}
	m.render(w, r, v)
}

func (m *EpisodeMappings) handleClear(w http.ResponseWriter, r *http.Request) {
	uid, v, p, ok := m.target(w, r)
	if !ok {
		return
	}
	v.Editing = false
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	err := m.write(r, uid, func() error { return m.store.ClearEpisodeMapping(ctx, uid, v.Context.Hash, v.Context.Index) })
	cancel()
	// Clearing an owned key does not depend on the metadata provider being up.
	read := r.Clone(r.Context())
	read.Method = http.MethodGet
	v = m.load(read, uid, v, p)
	v.Focus = true
	if err != nil {
		slog.Warn("clearing episode confirmation failed", "error", logError(err))
		v.Problem = i18n.T(r.Context(), "Episode confirmation could not be cleared. Please try again.")
	} else {
		v.Message = i18n.T(r.Context(), "Episode confirmation cleared.")
	}
	m.render(w, r, v)
}

// Validate membership before allocating anything from a submitted range.
func validMappingRange(v views.EpisodeMappingView) bool {
	if v.Season == nil || v.First <= 0 || v.Last < v.First || v.Last-v.First+1 > len(v.Season.Episodes) {
		return false
	}
	seen := map[int]bool{}
	for _, episode := range v.Season.Episodes {
		if episode.SeasonNumber == v.SelectedSeason && episode.Number >= v.First && episode.Number <= v.Last {
			seen[episode.Number] = true
		}
	}
	return len(seen) == v.Last-v.First+1
}

func (m *EpisodeMappings) render(w http.ResponseWriter, r *http.Request, v views.EpisodeMappingView) {
	sse := datastar.NewSSE(w, r, func(_ *datastar.ServerSentEventGenerator) { w.Header().Set("Cache-Control", "no-store") })
	selector := fmt.Sprintf(`#episode-mapping[data-file-key=%q]`, v.Context.Key())
	if err := sse.PatchElementTempl(views.EpisodeMappingPanel(v), datastar.WithSelector(selector), datastar.WithModeInner()); err != nil {
		logSSEError(r, "patch episode mapping", err)
	}
}
