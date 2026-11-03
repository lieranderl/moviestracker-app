package handlers

import (
	"context"
	"fmt"
	"github.com/a-h/templ"
	"net/http"
	"strconv"
	"strings"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/views"
	"github.com/starfederation/datastar-go/datastar"
)

// Completion reads a movie offer; opening or dismissing it never writes history.
type Completion struct {
	progress store.ProgressStore
	reviews  store.ReviewStore
	titles   store.TitleStateStore
	user     func(*http.Request) (string, bool)
}

func NewCompletion(progress store.ProgressStore, reviews store.ReviewStore, titles store.TitleStateStore, user func(*http.Request) (string, bool)) *Completion {
	return &Completion{progress: progress, reviews: reviews, titles: titles, user: user}
}

func (c *Completion) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/player/completion", c.handle)
}

func (c *Completion) handle(w http.ResponseWriter, r *http.Request) {
	uid, ok := c.user(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	hash := strings.ToLower(r.URL.Query().Get("hash"))
	index, ie := strconv.Atoi(r.URL.Query().Get("index"))
	nonce, ne := strconv.Atoi(r.URL.Query().Get("nonce"))
	if !infoHash.MatchString(hash) || ie != nil || ne != nil || index < 1 || nonce < 1 {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	p, found, err := c.progress.Progress(ctx, uid, hash, index)
	if err != nil {
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}
	if !found || p.Kind != "movie" || p.TMDBID < 1 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	review, err := c.reviews.Review(ctx, uid, "movie", p.TMDBID)
	if err != nil {
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}
	state, err := c.titles.TitleState(ctx, uid, "movie", p.TMDBID)
	if err != nil {
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}
	key := fmt.Sprintf("c_%s_%d_%d", hash, index, nonce)
	r = r.WithContext(views.WithPlayerControls(r.Context(), key))
	sse := datastar.NewSSE(w, r, func(_ *datastar.ServerSentEventGenerator) { w.Header().Set("Cache-Control", "no-store") })
	if err := sse.PatchElementTempl(views.PlayerCompletion(key, p.Title, p.TMDBID, review, state.State), datastar.WithSelector(`#player-completion[data-request-key="`+key+`"]`)); err != nil {
		logSSEError(r, "patch completion offer", err)
	}
}

// patchPlayerActionFailure retains the chosen review signals for explicit retry.
func patchPlayerActionFailure(w http.ResponseWriter, r *http.Request, fragment templ.Component) bool {
	if r.URL.Query().Get("player") == "" {
		return false
	}
	sse := datastar.NewSSE(w, r, func(_ *datastar.ServerSentEventGenerator) { w.Header().Set("Cache-Control", "no-store") })
	if err := sse.PatchElementTempl(fragment); err != nil {
		logSSEError(r, "patch player action failure", err)
	}
	return true
}
