package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/http"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// Recommendation limits: rows on the home page, titles whose TMDB
// recommendations are looked up to fill them, and lookups at once.
const (
	recommendationRows    = 3
	recommendationSeeds   = 6
	recommendationLookups = 3
)

// Recommendations fill the home page with titles like those the user
// watched or saved, for both apps.
type Recommendations struct {
	titles store.TitleStateStore
	user   func(*http.Request) (uid string, ok bool)
	// favorites are the user's favourites; nil where none are kept (the
	// local app).
	favorites func(ctx context.Context, uid string) ([]store.Favorite, error)
	catalog   *Catalog
}

// NewRecommendations recommends from the titles users marked in titles
// and, when favorites is set, their favourites.
func NewRecommendations(titles store.TitleStateStore, user func(*http.Request) (string, bool), favorites func(context.Context, string) ([]store.Favorite, error), catalog *Catalog) *Recommendations {
	return &Recommendations{titles: titles, user: user, favorites: favorites, catalog: catalog}
}

// Register adds the recommendations' route to mux.
func (rc *Recommendations) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/recommendations", rc.handleRows)
}

// seed is a title recommendations start from.
type seed struct {
	kind    string
	id      int
	title   string
	watched bool
}

func titleKey(kind string, id int) string { return fmt.Sprintf("%s-%d", kind, id) }

// seeds are the titles to start from, watched ones first, then favourites
// and Want to watch, newest first; marked is every title the user marked.
func (rc *Recommendations) seeds(ctx context.Context, uid string) (seeds []seed, marked map[string]bool, err error) {
	marked = map[string]bool{}
	add := func(kind string, id int, title string, watched bool) {
		if !marked[titleKey(kind, id)] {
			seeds = append(seeds, seed{kind, id, title, watched})
		}
		marked[titleKey(kind, id)] = true
	}
	watched, err := rc.titles.TitleStates(ctx, uid, store.Watched)
	if err != nil {
		return nil, nil, err
	}
	for _, t := range watched {
		add(t.Kind, t.TMDBID, t.Title, true)
	}
	if rc.favorites != nil {
		favs, err := rc.favorites(ctx, uid)
		if err != nil {
			return nil, nil, err
		}
		for _, f := range favs {
			add(f.Kind, f.TMDBID, f.Title, false)
		}
	}
	want, err := rc.titles.TitleStates(ctx, uid, store.WantToWatch)
	if err != nil {
		return nil, nil, err
	}
	for _, t := range want {
		add(t.Kind, t.TMDBID, t.Title, false)
	}
	return seeds, marked, nil
}

// related are TMDB's recommendations for a title.
func (c *Catalog) related(ctx context.Context, kind string, id int) ([]tmdb.MediaItem, error) {
	details := c.clients().Details
	if details == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, c.catalogTimeout)
	defer cancel()
	if kind == "tv" {
		tv, err := details.TV(ctx, id)
		if err != nil {
			return nil, err
		}
		return tv.Recommendations, nil
	}
	movie, err := details.Movie(ctx, id)
	if err != nil {
		return nil, err
	}
	return movie.Recommendations, nil
}

// recommendationRows are the rows seeds make, in their order: each holds
// TMDB's recommendations for its seed (related[i], answered[i] when its
// lookup finished) that the user has not marked and no row above shows.
// With waiting, they stop at the first seed still being looked up.
func recommendationRowsFor(ctx context.Context, seeds []seed, related [][]tmdb.MediaItem, answered []bool, marked map[string]bool, waiting bool) []views.RecommendationRow {
	shown := maps.Clone(marked)
	var rows []views.RecommendationRow
	for i, s := range seeds {
		if len(rows) == recommendationRows || (waiting && !answered[i]) {
			break
		}
		row := views.RecommendationRow{Title: i18n.Tf(ctx, "Because you saved %s", s.title)}
		if s.watched {
			row.Title = i18n.Tf(ctx, "Because you watched %s", s.title)
		}
		for _, it := range related[i] {
			if key := titleKey(it.MediaType, it.ID); !shown[key] {
				shown[key] = true
				row.Items = append(row.Items, it)
			}
		}
		if len(row.Items) > 0 {
			rows = append(rows, row)
		}
	}
	return rows
}

// rows looks up TMDB's recommendations for the first seeds, a few at a
// time and all within one deadline (the catalog timeout). Each lookup gets
// half of it, so titles TMDB is slow on give their turn to the next ones.
// Once the seeds answered in order fill the rows, the other lookups stop;
// at the deadline the rows are made of what was answered.
func (rc *Recommendations) rows(ctx context.Context, seeds []seed, marked map[string]bool) []views.RecommendationRow {
	seeds = seeds[:min(len(seeds), recommendationSeeds)]
	budget := rc.catalog.catalogTimeout
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	type answer struct {
		i       int
		related []tmdb.MediaItem
		err     error
	}
	answers := make(chan answer, len(seeds)) // lookups never wait on a reader gone
	related := make([][]tmdb.MediaItem, len(seeds))
	answered := make([]bool, len(seeds))
	next, running := 0, 0
	for {
		for ; running < recommendationLookups && next < len(seeds); next, running = next+1, running+1 {
			go func(i int) {
				lookup, done := context.WithTimeout(ctx, budget/2)
				defer done()
				items, err := rc.catalog.related(lookup, seeds[i].kind, seeds[i].id)
				answers <- answer{i, items, err}
			}(next)
		}
		if running == 0 {
			return recommendationRowsFor(ctx, seeds, related, answered, marked, false)
		}
		take := func(a answer) {
			running--
			answered[a.i] = true
			related[a.i] = a.related
			if a.err != nil {
				slog.Warn("tmdb recommendations failed", "kind", seeds[a.i].kind, "id", seeds[a.i].id, "error", logError(a.err))
			}
		}
		select {
		case a := <-answers:
			take(a)
			if rows := recommendationRowsFor(ctx, seeds, related, answered, marked, true); len(rows) == recommendationRows {
				return rows
			}
		case <-ctx.Done():
			for len(answers) > 0 { // answered just in time
				take(<-answers)
			}
			slog.Warn("tmdb recommendations ran out of time", "unanswered", running, "not looked up", len(seeds)-next)
			return recommendationRowsFor(ctx, seeds, related, answered, marked, false)
		}
	}
}

// handleRows patches the home page's recommendation rows: one per title
// the user watched or saved, holding TMDB's recommendations for it.
func (rc *Recommendations) handleRows(w http.ResponseWriter, r *http.Request) {
	uid, ok := rc.user(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	seeds, marked, err := rc.seeds(ctx, uid)
	cancel()
	if err != nil {
		slog.Warn("reading what to recommend from failed", "error", logError(err))
	}
	rows := rc.rows(r.Context(), seeds, marked)
	w.Header().Set("Cache-Control", "no-store")
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.RecommendationRows(rows)); err != nil {
		slog.Warn("patching recommendations failed", "error", err)
	}
}
