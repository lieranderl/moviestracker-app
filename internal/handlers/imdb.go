package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/views"

	"github.com/starfederation/datastar-go/datastar"
)

// maxRatingsPerRequest bounds one batched request; the hero rotates 7 titles.
const maxRatingsPerRequest = 10

type imdbRatingResult struct {
	target, rating, votes string
}

// handleMovieIMDbRating patches IMDb rating badges. It takes one or more
// id/target pairs (?id=tt1&target=a&id=tt2&target=b), so the home hero fills
// every slide's badge with one request, each patched as its rating arrives.
// A failed lookup patches an empty badge.
func (c *Catalog) handleMovieIMDbRating(w http.ResponseWriter, r *http.Request) {
	if c.apiUser(w, r) == nil {
		return
	}
	if c.clients().IMDb == nil {
		http.NotFound(w, r)
		return
	}

	q := r.URL.Query()
	ids, targets := q["id"], q["target"]
	if len(ids) == 0 || ids[0] == "" {
		http.Error(w, "missing imdb id", http.StatusBadRequest)
		return
	}
	ids = ids[:min(len(ids), maxRatingsPerRequest)]

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	// Buffered so lookups never block, even if the client goes away early.
	results := make(chan imdbRatingResult, len(ids))
	for i, imdbID := range ids {
		target := "hero-imdb-rating"
		if i < len(targets) && targets[i] != "" {
			target = targets[i]
		}
		go func() {
			rating, err := c.clients().IMDb.GetRating(ctx, imdbID)
			if err != nil {
				slog.Warn("failed to fetch imdb rating", "imdb_id", logValue(imdbID), "error", logError(err))
				results <- imdbRatingResult{target: target}
				return
			}
			results <- imdbRatingResult{target: target, rating: rating.Rating, votes: rating.FormattedVotes()}
		}()
	}

	sse := datastar.NewSSE(w, r)
	for range ids {
		res := <-results
		if err := sse.PatchElementTempl(views.IMDbRatingFragment(res.target, res.rating, res.votes)); err != nil {
			logSSEError(r, "patch imdb rating", err)
			return
		}
	}
}
