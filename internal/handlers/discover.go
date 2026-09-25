package handlers

import (
	"log/slog"
	"net/http"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"

	"github.com/starfederation/datastar-go/datastar"
)

type discoverResult struct {
	rail  views.DiscoverRail
	items []tmdb.MediaItem
}

// handleDiscover fetches every home discovery list concurrently and patches
// each rail as soon as its list arrives; failed lists collapse their rail.
func (s *Server) handleDiscover(w http.ResponseWriter, r *http.Request) {
	if s.apiUser(w, r) == nil {
		return
	}
	sse := datastar.NewSSE(w, r)
	ctx, cancel := s.detailsContext(r)
	defer cancel()

	// Buffered so fetchers never block, even if the client goes away early.
	results := make(chan discoverResult, len(views.DiscoverRails))
	for _, rail := range views.DiscoverRails {
		go func() {
			var items []tmdb.MediaItem
			if s.clients().Details != nil {
				var err error
				if items, err = s.clients().Details.List(ctx, rail.List); err != nil {
					slog.Warn("tmdb discover list failed", "list", rail.List, "error", err)
				}
			}
			results <- discoverResult{rail: rail, items: items}
		}()
	}
	for range views.DiscoverRails {
		res := <-results
		if err := sse.PatchElementTempl(views.DiscoverRailLoaded(res.rail, res.items)); err != nil {
			logSSEError(r, "patch discover rail", err)
			return
		}
	}
}
