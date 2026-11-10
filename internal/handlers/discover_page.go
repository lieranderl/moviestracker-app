package handlers

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/a-h/templ"
	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/moviestracker-app/internal/store"
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
	if q.Get("source") == "watchlist" {
		v.Source = "watchlist"
	}
	for _, raw := range q["pin"] {
		if id, ok := positiveInt(raw); ok && !slices.Contains(v.Pins, id) && len(v.Pins) < views.PickSize {
			v.Pins = append(v.Pins, id)
		}
	}
	if kind := q.Get("pinType"); kind != "" && kind != v.Query.MediaType {
		v.Pins = nil
		v.PickNotice = "Some pinned titles no longer match your choices."
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
		if v.Source == "watchlist" {
			p.Items, v.PickRuntimes, v.PickLimited, err = c.watchlistPool(ctx, r, v.Query)
		} else {
			p, err = c.clients().Details.Discover(ctx, v.Query, 1)
		}
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
			if v.Source == "watchlist" {
				v.Picks = drawPicks(p.Items, v.Pins, v.Pick)
			} else {
				v.Picks = c.pickForTonight(ctx, r, v.Query, p, v.Pick, v.Pins)
			}
			validPins := make([]int, 0, len(v.Pins))
			for _, it := range v.Picks {
				if slices.Contains(v.Pins, it.ID) {
					validPins = append(validPins, it.ID)
				}
			}
			if len(validPins) < len(v.Pins) {
				v.PickNotice = "Some pinned titles no longer match your choices."
			}
			v.Pins = validPins
			if v.PickRuntimes == nil {
				v.PickRuntimes = map[int]int{}
				for _, it := range v.Picks {
					_, minutes, _, _, e := c.pickMetadata(ctx, it.MediaType, it.ID)
					if e == nil && minutes > 0 {
						v.PickRuntimes[it.ID] = minutes
					}
				}
			}
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
func (c *Catalog) pickForTonight(ctx context.Context, r *http.Request, q tmdb.DiscoverQuery, first tmdb.Page, draw int, pins []int) []tmdb.MediaItem {
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
	return drawPicks(pool, pins, draw)
}

func drawPicks(pool []tmdb.MediaItem, pins []int, draw int) []tmdb.MediaItem {
	pool = slices.Clone(pool)
	rng := rand.New(rand.NewPCG(uint64(max(draw, 0)), 0)) // #nosec G115 G404 -- draw is positive; a shuffle, not a secret
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	out := make([]tmdb.MediaItem, 0, views.PickSize)
	for _, id := range pins {
		for _, it := range pool {
			if it.ID == id && len(out) < views.PickSize {
				out = append(out, it)
				break
			}
		}
	}
	for _, it := range pool {
		if len(out) == views.PickSize {
			break
		}
		if !slices.ContainsFunc(out, func(p tmdb.MediaItem) bool { return p.ID == it.ID }) {
			out = append(out, it)
		}
	}
	return out
}

// watchlistPool reads only this account's saved candidates. Metadata failures
// fail the request instead of silently treating unknown filters as matches.
func (c *Catalog) watchlistPool(ctx context.Context, r *http.Request, q tmdb.DiscoverQuery) ([]tmdb.MediaItem, map[int]int, bool, error) {
	if c.watchlist == nil || c.pickUser == nil {
		return nil, nil, false, errors.New("watchlist is unavailable")
	}
	uid, ok := c.pickUser(r)
	if !ok {
		return nil, nil, false, errors.New("watchlist requires sign-in")
	}
	saved, err := c.watchlist.TitleStates(ctx, uid, store.WantToWatch)
	if err != nil {
		return nil, nil, false, err
	}
	var candidates []store.TitleState
	for _, t := range saved {
		if t.Kind == q.MediaType {
			candidates = append(candidates, t)
		}
	}
	limited := len(candidates) > 50
	if limited {
		candidates = candidates[:50]
	}
	type candidate struct {
		item     tmdb.MediaItem
		minutes  int
		genres   []tmdb.Genre
		language string
		err      error
	}
	hydrated := make([]candidate, len(candidates))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(5, len(candidates)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				t := candidates[i]
				h := &hydrated[i]
				h.item, h.minutes, h.genres, h.language, h.err = c.pickMetadata(ctx, t.Kind, t.TMDBID)
			}
		}()
	}
	for i := range candidates {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return nil, nil, false, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	var pool []tmdb.MediaItem
	runtimes := map[int]int{}
	for _, h := range hydrated {
		if h.err != nil {
			return nil, nil, false, h.err
		}
		it, minutes, genres, language := h.item, h.minutes, h.genres, h.language
		if q.Genre > 0 && !slices.ContainsFunc(genres, func(g tmdb.Genre) bool { return g.ID == q.Genre }) {
			continue
		}
		if q.Year > 0 && it.ReleaseYear() != strconv.Itoa(q.Year) {
			continue
		}
		if q.MinRating > 0 && it.VoteAverage < q.MinRating {
			continue
		}
		if q.MaxRuntime > 0 && (minutes <= 0 || minutes > q.MaxRuntime) {
			continue
		}
		if q.Language != "" && language != q.Language {
			continue
		}
		pool = append(pool, it)
		runtimes[it.ID] = minutes
	}
	switch q.Sort {
	case "rating":
		slices.SortStableFunc(pool, func(a, b tmdb.MediaItem) int { return cmp.Compare(b.VoteAverage, a.VoteAverage) })
	case "newest":
		slices.SortStableFunc(pool, func(a, b tmdb.MediaItem) int { return cmp.Compare(b.ReleaseDate, a.ReleaseDate) })
	}
	return pool, runtimes, limited, nil
}

func (c *Catalog) pickMetadata(ctx context.Context, kind string, id int) (tmdb.MediaItem, int, []tmdb.Genre, string, error) {
	if kind == "tv" {
		d, e := c.clients().Details.TV(ctx, id)
		if e != nil {
			return tmdb.MediaItem{}, 0, nil, "", e
		}
		return d.MediaItem, d.EpisodeRuntime, d.Genres, d.OriginalLanguage, nil
	}
	d, e := c.clients().Details.Movie(ctx, id)
	if e != nil {
		return tmdb.MediaItem{}, 0, nil, "", e
	}
	return d.MediaItem, d.Runtime, d.Genres, d.OriginalLanguage, nil
}
