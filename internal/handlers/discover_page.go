package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/a-h/templ"
	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// discoverView reads Discover's filters from r, keeping only those its
// form can show: the kind of title, one of its genres, a year it offers, a
// rating it offers and a sort. It fails when TMDB does not give the genres
// it needs to check a genre filter: showing the titles unfiltered under a
// filtered address would mislead.
func (c *Catalog) discoverView(ctx context.Context, r *http.Request) (views.DiscoverView, error) {
	q := r.URL.Query()
	thisYear := time.Now().Year()
	v := views.DiscoverView{Query: tmdb.DiscoverQuery{MediaType: "movie", Sort: "popular"}, ThisYear: thisYear}
	if q.Get("type") == "tv" {
		v.Query.MediaType = "tv"
	}
	genres, err := c.clients().Details.Genres(ctx, v.Query.MediaType)
	if err != nil {
		slog.Warn("tmdb genres failed", "type", v.Query.MediaType, "error", err)
		if q.Get("genre") != "" {
			return v, err
		}
	}
	v.Genres = genres
	if id, ok := positiveInt(q.Get("genre")); ok && slices.ContainsFunc(genres, func(g tmdb.Genre) bool { return g.ID == id }) {
		v.Query.Genre = id
	}
	if y, ok := positiveInt(q.Get("year")); ok && y >= views.DiscoverFirstYear && y <= thisYear {
		v.Query.Year = y
	}
	if rating, err := strconv.ParseFloat(q.Get("rating"), 64); err == nil && slices.Contains(views.DiscoverRatings, rating) {
		v.Query.MinRating = rating
	}
	switch sort := q.Get("sort"); sort {
	case "rating", "newest":
		v.Query.Sort = sort
	}
	return v, nil
}

// handleDiscoverPage is Discover: TMDB's catalog filtered, its first page.
func (c *Catalog) handleDiscoverPage(w http.ResponseWriter, r *http.Request) {
	user := c.userFromRequest(r)
	if user == nil {
		http.Redirect(w, r, c.signIn, http.StatusSeeOther)
		return
	}
	if c.clients().Details == nil {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := c.detailsContext(r)
	defer cancel()
	v, err := c.discoverView(ctx, r)
	var p tmdb.Page
	if err == nil {
		p, err = c.clients().Details.Discover(ctx, v.Query, 1)
	}
	if err != nil {
		slog.Warn("tmdb discover failed", "query", v.Query, "error", err)
		v.Failed = true
	} else {
		v.Items = p.Items
		if p.TotalPages > 1 {
			v.Next = 2
		}
	}
	templ.Handler(views.DiscoverPage(user, v)).ServeHTTP(w, r)
}

// handleDiscoverMore appends ?page= of Discover's titles to the grid and
// moves the loader under it to the page after, until TMDB's last page.
func (c *Catalog) handleDiscoverMore(w http.ResponseWriter, r *http.Request) {
	if c.apiUser(w, r) == nil {
		return
	}
	details := c.clients().Details
	if details == nil {
		http.NotFound(w, r)
		return
	}
	page, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || page < 2 || page > tmdb.MaxPage {
		http.Error(w, "Ask for a page from 2 to 500.", http.StatusBadRequest)
		return
	}
	ctx, cancel := c.detailsContext(r)
	defer cancel()
	v, err := c.discoverView(ctx, r)
	var p tmdb.Page
	if err == nil {
		p, err = details.Discover(ctx, v.Query, page)
	}
	sse := datastar.NewSSE(w, r)
	if err != nil {
		slog.Warn("tmdb discover page failed", "query", v.Query, "page", page, "error", err)
		v.Failed, v.Next = true, page
		if err := sse.PatchElementTempl(views.DiscoverMore(v)); err != nil {
			logSSEError(r, "patch discover retry", err)
		}
		return
	}
	// TMDB's pages shift as titles move up and down: what the page before
	// showed is not shown again.
	shown := map[int]bool{}
	if prev, err := details.Discover(ctx, v.Query, page-1); err == nil {
		for _, it := range prev.Items {
			shown[it.ID] = true
		}
	}
	var items []tmdb.MediaItem
	for _, it := range p.Items {
		if !shown[it.ID] {
			items = append(items, it)
		}
	}
	if page < p.TotalPages {
		v.Next = page + 1
	}
	if err := sse.PatchElementTempl(views.BrowseItems(items), datastar.WithSelectorID("browse-grid"), datastar.WithModeAppend()); err != nil {
		logSSEError(r, "patch discover page", err)
		return
	}
	if err := sse.PatchElementTempl(views.DiscoverMore(v)); err != nil {
		logSSEError(r, "patch discover loader", err)
	}
}
