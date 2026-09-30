package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/views"

	"github.com/a-h/templ"
	"github.com/starfederation/datastar-go/datastar"
)

const maxSearchQuery = 100

type searchSignals struct {
	Q string `json:"q"`
}

func normalizeQuery(q string) string {
	q = strings.Join(strings.Fields(q), " ")
	if r := []rune(q); len(r) > maxSearchQuery {
		q = string(r[:maxSearchQuery])
	}
	return q
}

func (c *Catalog) handleSearchPage(w http.ResponseWriter, r *http.Request) {
	user := c.pageUser(w, r)
	if user == nil {
		return
	}
	ctx, cancel := c.detailsContext(r)
	defer cancel()
	view := c.searchView(ctx, normalizeQuery(r.URL.Query().Get("q")))
	templ.Handler(views.SearchPage(user, view)).ServeHTTP(w, r)
}

// handleSearchAPI patches live results for the $q signal.
func (c *Catalog) handleSearchAPI(w http.ResponseWriter, r *http.Request) {
	if c.apiUser(w, r) == nil {
		return
	}
	var signals searchSignals
	if err := datastar.ReadSignals(r, &signals); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	ctx, cancel := c.detailsContext(r)
	defer cancel()
	view := c.searchView(ctx, normalizeQuery(signals.Q))
	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(views.SearchResults(view)); err != nil {
		logSSEError(r, "patch search results", err)
	}
}

// searchView runs a TMDB multi-search, or gathers trending rails for a blank query.
func (c *Catalog) searchView(ctx context.Context, q string) views.SearchView {
	view := views.SearchView{Query: q}
	if q == "" {
		if c.clients().Catalog != nil {
			catalog, err := c.clients().Catalog.GetCatalog(ctx)
			if err != nil {
				slog.Warn("search discovery catalog unavailable", "error", err)
			}
			view.TrendingMovies, view.TrendingSeries = catalog.Movies, catalog.Series
		}
		return view
	}
	if c.clients().Details == nil {
		view.Err = i18n.T(ctx, "Search is unavailable because TMDB is not configured.")
		return view
	}
	results, err := c.clients().Details.Search(ctx, q)
	if err != nil {
		slog.Warn("tmdb search failed", "error", err)
		view.Err = i18n.T(ctx, "TMDB search is not responding right now. Please try again.")
		return view
	}
	view.Results = results
	return view
}
