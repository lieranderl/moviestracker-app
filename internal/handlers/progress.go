package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"
	"github.com/starfederation/datastar-go/datastar"
)

// resumeAfter is how far into a file the player offers to resume it.
const resumeAfter = time.Minute

// progressTimeout bounds each read or write of a user's progress.
const progressTimeout = 5 * time.Second

var infoHash = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)

// Progress serves where users stopped watching, for both apps: the player
// reports it, and pages offer to resume.
type Progress struct {
	store store.ProgressStore
	user  func(*http.Request) (uid string, ok bool)
	// torrent finds a torrent in the TorrServer in use; nil when the
	// browser relays it with each report (the web app).
	torrent func(ctx context.Context, hash string) (torrserver.Torrent, bool)
	// timecodes is the TorrServer in use, which keeps a timecode per file
	// for its other players; nil when the browser talks to it (the web app).
	timecodes func() Timecodes
	// watched marks movies watched once finished; nil marks nothing.
	watched *TitleStates
}

// MarkFinishedMoviesIn marks a movie watched in ts when the player reports
// it finished. Series are left alone: one episode is not the series.
func (p *Progress) MarkFinishedMoviesIn(ts *TitleStates) { p.watched = ts }

// Timecodes keeps where files were left in a TorrServer (its /viewed API).
type Timecodes interface {
	SetViewed(ctx context.Context, hash string, index int, seconds float64) error
	Timecode(ctx context.Context, hash string, index int) (time.Duration, error)
}

// MirrorTo keeps the TorrServer in use told where users stop, and resumes
// files only it saw played from its timecode.
func (p *Progress) MirrorTo(timecodes func() Timecodes) { p.timecodes = timecodes }

// NewProgress keeps progress in s for the users user names. torrent, when
// set, looks up the torrent a report names; when nil, reports carry it.
func NewProgress(s store.ProgressStore, user func(*http.Request) (string, bool), torrent func(context.Context, string) (torrserver.Torrent, bool)) *Progress {
	return &Progress{store: s, user: user, torrent: torrent}
}

// continueLimit is how many unfinished files the home page lists.
const continueLimit = 12

// titleWindow is how many unfinished files a title page looks through.
const titleWindow = 50

// recent is what the signed-in user has not finished, newest first, for
// the home page.
func (p *Progress) recent(r *http.Request) []store.Progress { return p.recentOf(r, continueLimit) }

// recentOf is up to limit files the signed-in user has not finished, newest
// first; none for a nil p, a signed-out visitor or a store that does not
// answer.
func (p *Progress) recentOf(r *http.Request, limit int) []store.Progress {
	if p == nil {
		return nil
	}
	uid, ok := p.user(r)
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	watching, err := p.store.RecentProgress(ctx, uid, limit)
	if err != nil {
		slog.Warn("reading recent progress failed", "error", logError(err))
	}
	return watching
}

// PlayRequest is the file a TorrServer page's link asks to play
// (?play=<hash>&file=<id>), titled as the user last watched it.
func (p *Progress) PlayRequest(r *http.Request) (views.PlayRequest, bool) {
	hash := r.URL.Query().Get("play")
	index, err := strconv.Atoi(r.URL.Query().Get("file"))
	if !infoHash.MatchString(hash) || err != nil || index < 1 {
		return views.PlayRequest{}, false
	}
	req := views.PlayRequest{Hash: strings.ToLower(hash), Index: index}
	if uid, ok := p.user(r); ok {
		ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
		defer cancel()
		if kept, found, err := p.store.Progress(ctx, uid, hash, index); err == nil && found {
			req.Title, req.Kind, req.TMDBID = kept.Title, kept.Kind, kept.TMDBID
		}
	}
	return req, true
}

// Register adds the progress routes to mux.
func (p *Progress) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/progress", p.handleSave)
	mux.HandleFunc("GET /api/progress/resume", p.handleResume)
	mux.HandleFunc("GET /api/progress/title", p.handleTitle)
}

// otherPlayers is where TorrServer's other players left a file: the local
// app asks TorrServer, the web app's browser sends it (?at=seconds).
func (p *Progress) otherPlayers(ctx context.Context, r *http.Request, hash string, index int) time.Duration {
	if p.timecodes == nil {
		at, err := strconv.ParseFloat(r.URL.Query().Get("at"), 64)
		if err != nil || at < 0 || at > math.MaxInt32 {
			return 0
		}
		return time.Duration(at) * time.Second
	}
	at, err := p.timecodes().Timecode(ctx, hash, index)
	if err != nil {
		slog.Warn("reading TorrServer's timecode failed", "error", err)
	}
	return at
}

// handleTitle patches a title page's button to continue the file of it the
// user last left unfinished, or an empty slot.
func (p *Progress) handleTitle(w http.ResponseWriter, r *http.Request) {
	kind := r.URL.Query().Get("type")
	id, err := strconv.Atoi(r.URL.Query().Get("id"))
	if (kind != "movie" && kind != "tv") || err != nil || id <= 0 {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	var last *store.Progress
	for _, kept := range p.recentOf(r, titleWindow) {
		if kept.Kind == kind && kept.TMDBID == id {
			last = &kept
			break
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.ProgressButton(last)); err != nil {
		slog.Warn("patching the title progress failed", "error", err)
	}
}

// handleResume patches the player's offer to resume a file where the user
// stopped, or clears it when they never played it or finished it.
func (p *Progress) handleResume(w http.ResponseWriter, r *http.Request) {
	uid, ok := p.user(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	hash := r.URL.Query().Get("hash")
	index, err := strconv.Atoi(r.URL.Query().Get("index"))
	if !infoHash.MatchString(hash) || err != nil || index < 1 {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	kept, found, err := p.store.Progress(ctx, uid, hash, index)
	if err != nil {
		slog.Warn("reading progress failed", "error", logError(err))
	}
	at := kept.Position
	if !found {
		at = p.otherPlayers(ctx, r, hash, index)
	}
	if kept.Finished() || at < resumeAfter {
		at = 0
	}
	w.Header().Set("Cache-Control", "no-store")
	sse := datastar.NewSSE(w, r)
	// The offer's state goes with every answer: a dismissed offer comes
	// back when its file opens again, and saving waits while it is pending.
	offer := ""
	if at > 0 {
		offer = fmt.Sprintf("%s-%d", strings.ToLower(hash), index)
	}
	if err := sse.MarshalAndPatchSignals(map[string]any{"_resume": offer, "_resumeAt": int(at / time.Second)}); err != nil {
		slog.Warn("patching the resume offer failed", "error", err)
		return
	}
	if err := sse.PatchElementTempl(views.TorrResume(hash, index, at)); err != nil {
		slog.Warn("patching the resume offer failed", "error", err)
	}
}

// handleSave keeps where the user is in a file, as the player reports it.
// The torrent names the file and the TMDB title it was added for.
func (p *Progress) handleSave(w http.ResponseWriter, r *http.Request) {
	uid, ok := p.user(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	var data struct {
		Hash     string              `json:"hash"`
		Index    int                 `json:"index"`
		Position float64             `json:"position"`
		Duration float64             `json:"duration"`
		Torrent  *torrserver.Torrent `json:"torrent"`
	}
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2*maxRequestBodyBytes)).Decode(&data)
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		http.Error(w, "Request Entity Too Large", http.StatusRequestEntityTooLarge)
		return
	}
	validTime := data.Position >= 0 && data.Duration > 0 && data.Position <= data.Duration && data.Duration < math.MaxInt32
	if err != nil || !infoHash.MatchString(data.Hash) || data.Index < 1 || !validTime {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	if p.torrent != nil {
		t, found := p.torrent(ctx, data.Hash)
		data.Torrent = nil
		if found {
			data.Torrent = &t
		}
	}
	if data.Torrent == nil || data.Torrent.Hash != data.Hash {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	rec := store.Progress{
		Hash: data.Hash, Index: data.Index, Title: data.Torrent.DisplayName(), Poster: data.Torrent.Poster,
		Position: time.Duration(data.Position) * time.Second, Duration: time.Duration(data.Duration) * time.Second,
	}
	rec.Kind, rec.TMDBID = data.Torrent.TMDB()
	for _, f := range data.Torrent.FileStats {
		if f.ID == data.Index {
			rec.File = f.Path
		}
	}
	if err := p.store.SaveProgress(ctx, uid, rec); err != nil {
		slog.Warn("saving progress failed", "error", logError(err))
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}
	if p.watched != nil && rec.Finished() && rec.Kind == "movie" {
		p.watched.markWatched(r, uid, rec.TMDBID, rec.Title)
	}
	if p.timecodes != nil {
		if err := p.timecodes().SetViewed(ctx, data.Hash, data.Index, math.Floor(data.Position)); err != nil {
			slog.Warn("telling TorrServer the timecode failed", "error", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
