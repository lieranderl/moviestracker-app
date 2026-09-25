package handlers

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"

	"github.com/a-h/templ"
	"github.com/starfederation/datastar-go/datastar"
)

// handleTVPage renders a series with the requested (or first) season's
// episodes inline, so the page is complete without JavaScript.
func (s *Server) handleTVPage(w http.ResponseWriter, r *http.Request) {
	user, id, ok := s.detailTarget(w, r)
	if !ok {
		return
	}
	ctx, cancel := s.detailsContext(r)
	defer cancel()
	tv, err := s.clients().Details.TV(ctx, id)
	if err != nil {
		renderMediaError(w, r, user, err)
		return
	}

	selected := tv.DefaultSeason()
	if n, err := strconv.Atoi(r.URL.Query().Get("season")); err == nil && hasSeason(tv, n) {
		selected = n
	}
	var season *tmdb.Season
	if len(tv.Seasons) > 0 {
		if season, err = s.clients().Details.Season(ctx, id, selected); err != nil {
			slog.Warn("tmdb season lookup failed", "tv", id, "season", selected, "error", err)
		}
	}
	templ.Handler(views.TVPage(user, tv, season, selected)).ServeHTTP(w, r)
}

func hasSeason(tv *tmdb.TVDetails, n int) bool {
	for _, s := range tv.Seasons {
		if s.Number == n {
			return true
		}
	}
	return false
}

// handleSeason patches one season's episode list and the $season signal.
func (s *Server) handleSeason(w http.ResponseWriter, r *http.Request) {
	if s.apiUser(w, r) == nil {
		return
	}
	id, idOK := positiveInt(r.PathValue("id"))
	number, err := strconv.Atoi(r.PathValue("season"))
	sse := datastar.NewSSE(w, r)
	if !idOK || err != nil || number < 0 || s.clients().Details == nil {
		patchSeasonError(r, sse)
		return
	}
	ctx, cancel := s.detailsContext(r)
	defer cancel()
	season, err := s.clients().Details.Season(ctx, id, number)
	if err != nil {
		slog.Warn("tmdb season lookup failed", "tv", id, "season", number, "error", err)
		patchSeasonError(r, sse)
		return
	}
	if err := sse.PatchElementTempl(views.SeasonEpisodes(season)); err != nil {
		logSSEError(r, "patch season episodes", err)
		return
	}
	if err := sse.MarshalAndPatchSignals(map[string]any{"season": number}); err != nil {
		logSSEError(r, "patch season signal", err)
	}
}

func patchSeasonError(r *http.Request, sse *datastar.ServerSentEventGenerator) {
	if err := sse.PatchElementTempl(views.SeasonError()); err != nil {
		logSSEError(r, "patch season error", err)
	}
}
