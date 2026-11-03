package handlers

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
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
	details := c.clients().Details
	if details == nil {
		return v, errors.New("catalog is unavailable")
	}
	genres, err := details.Genres(ctx, v.Query.MediaType)
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
	if m, ok := positiveInt(q.Get("runtime")); ok && slices.Contains(views.DiscoverRuntimes, m) {
		v.Query.MaxRuntime = m
	}
	for _, l := range views.DiscoverLanguages {
		if l.Code == q.Get("lang") {
			v.Query.Language = l.Code
		}
	}
	if draw, ok := positiveInt(q.Get("pick")); ok {
		v.Pick = draw
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
		if v.Pick > 0 {
			v.Picks = c.pickForTonight(ctx, r, v.Query, p, v.Pick)
		}
	}
	if c.presets != nil {
		v.Presets = c.presets.pageState(r, v.FilterParams())
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

// pickPages is how many pages of Discover a shortlist for tonight draws
// from.
const pickPages = 3

// pickForTonight is a shortlist of up to views.PickSize titles q finds that
// the user has not marked, drawn (draw: the reroll's number) from the
// first pickPages pages, first among them.
func (c *Catalog) pickForTonight(ctx context.Context, r *http.Request, q tmdb.DiscoverQuery, first tmdb.Page, draw int) []tmdb.MediaItem {
	marked := map[string]bool{}
	if c.marked != nil {
		marked = c.marked(r)
	}
	var pool []tmdb.MediaItem
	seen := map[string]bool{} // rankings shift between pages, repeating titles
	page := first
	for n := 1; ; n++ {
		for _, it := range page.Items {
			if k := titleKey(it.MediaType, it.ID); !marked[k] && !seen[k] {
				seen[k] = true
				pool = append(pool, it)
			}
		}
		if n >= pickPages || n >= page.TotalPages {
			break
		}
		next, err := c.clients().Details.Discover(ctx, q, n+1)
		if err != nil {
			slog.Warn("tmdb discover failed", "query", q, "error", err)
			break
		}
		page = next
	}
	rng := rand.New(rand.NewPCG(uint64(max(draw, 0)), 0)) // #nosec G115 G404 -- draw is positive; a shuffle, not a secret
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	return pool[:min(len(pool), views.PickSize)]
}
