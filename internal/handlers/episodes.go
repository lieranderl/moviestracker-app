package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// Episodes lets users mark the episodes of a series watched, for both apps.
type Episodes struct {
	store   store.EpisodeStore
	user    func(*http.Request) (uid string, ok bool)
	catalog *Catalog
}

// NewEpisodes keeps users' watched episodes in s and shows them on the
// catalog's series pages.
func NewEpisodes(s store.EpisodeStore, user func(*http.Request) (string, bool), catalog *Catalog) *Episodes {
	e := &Episodes{store: s, user: user, catalog: catalog}
	catalog.episodes = e
	return e
}

// Register adds the episode routes to mux.
func (e *Episodes) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/episodes", e.handleMark)
	mux.HandleFunc("POST /api/episodes/season", e.handleMark)
	mux.HandleFunc("POST /api/episodes/spoilers", e.handleSpoilers)
	mux.HandleFunc("GET /api/episodes/reveal", e.handleReveal)
}

// spoilersCookie keeps whether a browser hides spoilers of unwatched
// episodes ("1").
const spoilersCookie = "mt_spoilers"

// hidesSpoilers says whether the request's browser hides spoilers.
func hidesSpoilers(r *http.Request) bool {
	c, err := r.Cookie(spoilersCookie)
	return err == nil && c.Value == "1"
}

// handleSpoilers turns spoiler protection on or off (?on=1|0) for the
// browser and patches the season (?tv=, ?season=) it was changed on.
func (e *Episodes) handleSpoilers(w http.ResponseWriter, r *http.Request) {
	on := r.URL.Query().Get("on") == "1"
	cookie := &http.Cookie{Name: spoilersCookie, Value: "0", Path: "/", MaxAge: consentMaxAge, HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode}
	// As the language cookie: Secure only where the request came over HTTPS.
	cookie.Secure = r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	if on {
		cookie.Value = "1"
	}
	http.SetCookie(w, cookie)
	s, tvID, ok := e.season(w, r)
	if !ok {
		return
	}
	marks := e.Marks(r, tvID)
	marks.HideSpoilers = on
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.SeasonEpisodes(s, marks)); err != nil {
		logSSEError(r, "patch season episodes", err)
	}
}

// handleReveal patches one episode (?tv=, ?season=, ?episode=) with its
// summary and still, whatever spoiler protection hides.
func (e *Episodes) handleReveal(w http.ResponseWriter, r *http.Request) {
	s, tvID, ok := e.season(w, r)
	if !ok {
		return
	}
	n, _ := positiveInt(r.URL.Query().Get("episode"))
	for _, ep := range s.Episodes {
		if ep.Number == n {
			if err := datastar.NewSSE(w, r).PatchElementTempl(views.EpisodeCard(tvID, ep, e.Marks(r, tvID), true)); err != nil {
				logSSEError(r, "patch episode", err)
			}
			return
		}
	}
	http.NotFound(w, r)
}

// season is the season a request names (?tv=, ?season=), from TMDB, for a
// signed-in user, as season loads are; it answers the request itself when
// there is none.
func (e *Episodes) season(w http.ResponseWriter, r *http.Request) (*tmdb.Season, int, bool) {
	if e.catalog.apiUser(w, r) == nil {
		return nil, 0, false
	}
	tvID, idOK := positiveInt(r.URL.Query().Get("tv"))
	number, err := strconv.Atoi(r.URL.Query().Get("season"))
	if !idOK || err != nil || number < 0 || e.catalog.clients().Details == nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return nil, 0, false
	}
	ctx, cancel := e.catalog.detailsContext(r)
	defer cancel()
	s, err := e.catalog.clients().Details.Season(ctx, tvID, number)
	if err != nil {
		slog.Warn("tmdb season lookup failed", "tv", tvID, "season", number, "error", err)
		http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
		return nil, 0, false
	}
	return s, tvID, true
}

// Marks are the request's user's watched episodes of series tvID; without
// a user, or when they cannot be read, none can be marked.
func (e *Episodes) Marks(r *http.Request, tvID int) views.EpisodeMarks {
	if e == nil {
		return views.EpisodeMarks{HideSpoilers: hidesSpoilers(r)}
	}
	uid, ok := e.user(r)
	if !ok {
		return views.EpisodeMarks{HideSpoilers: hidesSpoilers(r)}
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	watched, err := e.store.WatchedEpisodes(ctx, uid, tvID)
	if err != nil {
		slog.Warn("reading watched episodes failed", "error", logError(err))
		return views.EpisodeMarks{HideSpoilers: hidesSpoilers(r)}
	}
	return views.EpisodeMarks{Track: true, Watched: watched, HideSpoilers: hidesSpoilers(r)}
}

// handleMark marks one episode (/api/episodes, ?episode=) or a season's
// aired episodes (/api/episodes/season) watched or not (?watched=1|0), then
// patches the season and the series' next unwatched episode.
func (e *Episodes) handleMark(w http.ResponseWriter, r *http.Request) {
	uid, ok := e.user(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	q := r.URL.Query()
	tvID, idOK := positiveInt(q.Get("tv"))
	season, err := strconv.Atoi(q.Get("season"))
	watched := q.Get("watched") == "1"
	if !idOK || err != nil || season < 0 || (q.Get("watched") != "1" && q.Get("watched") != "0") || e.catalog.clients().Details == nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	ctx, cancel := e.catalog.detailsContext(r)
	defer cancel()
	details := e.catalog.clients().Details
	s, err := details.Season(ctx, tvID, season)
	if err != nil {
		slog.Warn("tmdb season lookup failed", "tv", tvID, "season", season, "error", err)
		http.Error(w, http.StatusText(http.StatusBadGateway), http.StatusBadGateway)
		return
	}
	var episodes []int
	if r.URL.Path == "/api/episodes" {
		n, ok := positiveInt(q.Get("episode"))
		if !ok {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		episodes = []int{n}
	} else {
		episodes = airedEpisodes(s)
	}
	if err := e.store.MarkEpisodes(ctx, uid, tvID, season, episodes, watched); err != nil {
		slog.Warn("marking episodes failed", "error", logError(err))
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}
	marks := e.Marks(r, tvID)
	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(views.SeasonEpisodes(s, marks)); err != nil {
		logSSEError(r, "patch season episodes", err)
		return
	}
	if tv, err := details.TV(ctx, tvID); err == nil {
		if err := sse.PatchElementTempl(views.NextUnwatched(tv, marks)); err != nil {
			logSSEError(r, "patch next unwatched", err)
		}
	}
}

// airedEpisodes are the numbers of the season's episodes that are out.
func airedEpisodes(s *tmdb.Season) []int {
	var out []int
	for _, ep := range s.Episodes {
		if views.EpisodeAired(ep) {
			out = append(out, ep.Number)
		}
	}
	return out
}
