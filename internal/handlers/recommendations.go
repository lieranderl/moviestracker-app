package handlers

import (
	"context"
	"fmt"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// Recommendation limits: titles the home page's row mixes and the row's
// length (a catalog rail's), titles whose TMDB recommendations are looked
// up to find them, and lookups at once.
const (
	recommendationSources = 3
	recommendationTitles  = 20
	recommendationSeeds   = 6
	recommendationLookups = 3
)

// Recommendations fill the home page with titles like those the user
// watched or saved, for both apps.
type Recommendations struct {
	library *Library
	user    func(*http.Request) (uid string, ok bool)
	catalog *Catalog
}

// NewRecommendations recommends from the users' libraries.
func NewRecommendations(library *Library, user func(*http.Request) (string, bool), catalog *Catalog) *Recommendations {
	return &Recommendations{library: library, user: user, catalog: catalog}
}

// Register adds the recommendations' route to mux.
func (rc *Recommendations) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/recommendations", rc.handleRows)
	mux.HandleFunc("POST /api/recommendations/feedback", rc.handleFeedback)
	mux.HandleFunc("POST /api/recommendations/feedback/reset", rc.handleFeedback)
}

// seed is a title recommendations start from.
type seed struct {
	kind    string
	id      int
	title   string
	watched bool
	more    bool // the user asked for more like it
}

func titleKey(kind string, id int) string { return fmt.Sprintf("%s-%d", kind, id) }

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

// recommendationRowsFor is the row seeds make: TMDB's recommendations
// (related[i], answered[i] when its lookup finished) for the first
// recommendationSources seeds that have any the user has not marked, a
// title from each in turn, none twice and at most recommendationTitles. With waiting, the seeds stop at
// the first still being looked up. full says the row has all its sources.
func recommendationRowsFor(ctx context.Context, seeds []seed, related [][]tmdb.MediaItem, answered []bool, marked map[string]bool, waiting bool) (rows []views.RecommendationRow, full bool) {
	var sources [][]tmdb.MediaItem
	var names []string
	watched := true
	for i, s := range seeds {
		if len(sources) == recommendationSources || (waiting && !answered[i]) {
			break
		}
		fresh := slices.DeleteFunc(slices.Clone(related[i]), func(it tmdb.MediaItem) bool { return marked[titleKey(it.MediaType, it.ID)] })
		if len(fresh) == 0 {
			continue
		}
		sources = append(sources, fresh)
		names = append(names, s.title)
		watched = watched && s.watched
	}
	if len(sources) == 0 {
		return nil, false
	}
	row := views.RecommendationRow{Title: i18n.Tf(ctx, "Based on %s", i18n.List(ctx, names))}
	if watched {
		row.Title = i18n.Tf(ctx, "Because you watched %s", i18n.List(ctx, names))
	}
	shown := map[string]bool{}
	for turn := 0; len(row.Items) < recommendationTitles; turn++ {
		took := false
		for _, items := range sources {
			if turn >= len(items) {
				continue
			}
			took = true
			if it := items[turn]; !shown[titleKey(it.MediaType, it.ID)] && len(row.Items) < recommendationTitles {
				shown[titleKey(it.MediaType, it.ID)] = true
				row.Items = append(row.Items, it)
			}
		}
		if !took {
			break
		}
	}
	return []views.RecommendationRow{row}, len(sources) == recommendationSources
}

// rows looks up TMDB's recommendations for the first seeds, a few at a
// time and all within one deadline (the catalog timeout). Each lookup gets
// half of it, so titles TMDB is slow on give their turn to the next ones.
// Once the seeds answered in order fill the row, the other lookups stop;
// at the deadline the row is made of what was answered.
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
			rows, _ := recommendationRowsFor(ctx, seeds, related, answered, marked, false)
			return rows
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
			if rows, full := recommendationRowsFor(ctx, seeds, related, answered, marked, true); full {
				return rows
			}
		case <-ctx.Done():
			for len(answers) > 0 { // answered just in time
				take(<-answers)
			}
			slog.Warn("tmdb recommendations ran out of time", "unanswered", running, "not looked up", len(seeds)-next)
			rows, _ := recommendationRowsFor(ctx, seeds, related, answered, marked, false)
			return rows
		}
	}
}

// handleRows patches the home page's recommendation row: TMDB's
// recommendations for the titles the user watched or saved last.
func (rc *Recommendations) handleRows(w http.ResponseWriter, r *http.Request) {
	uid, ok := rc.user(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	rc.patchRows(w, r, uid, nil)
}

// patchRows patches the rows as the user's library and feedback make them
// now; undone is a title just hidden, offered back.
func (rc *Recommendations) patchRows(w http.ResponseWriter, r *http.Request, uid string, undone *tmdb.MediaItem) {
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	library, err := rc.library.Summary(ctx, uid)
	cancel()
	if err != nil {
		slog.Warn("reading what to recommend from failed", "error", logError(err))
	}
	rows := rc.rows(r.Context(), rc.localizeSeeds(r.Context(), library.Seeds), library.Marked)
	feedback := views.RecommendationFeedback{Undone: undone, Hidden: library.Hidden, Given: library.Given, Allow: rc.library.feedback != nil}
	w.Header().Set("Cache-Control", "no-store")
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.RecommendationRows(rows, feedback)); err != nil {
		slog.Warn("patching recommendations failed", "error", err)
	}
}

// handleFeedback gives or takes back the user's feedback on a recommended
// title (?type=, ?id=, ?feedback=notInterested|moreLikeThis, ?on=1|0), or
// forgets it all (/reset), and patches the rows.
func (rc *Recommendations) handleFeedback(w http.ResponseWriter, r *http.Request) {
	uid, ok := rc.user(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	feedback := rc.library.feedback
	if feedback == nil {
		http.NotFound(w, r)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/reset") {
		ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
		defer cancel()
		if err := feedback.ResetFeedback(ctx, uid); err != nil {
			slog.Warn("resetting recommendation feedback failed", "error", logError(err))
			http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			return
		}
		rc.patchRows(w, r, uid, nil)
		return
	}
	q := r.URL.Query()
	id, idOK := positiveInt(q.Get("id"))
	kind, which, on := q.Get("type"), store.Feedback(q.Get("feedback")), q.Get("on") == "1"
	if !idOK || (kind != "movie" && kind != "tv") || (which != store.NotInterested && which != store.MoreLikeThis) {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	item := tmdb.MediaItem{ID: id, MediaType: kind}
	if on { // named for the undo notice; taking feedback back needs no name
		if named, err := rc.catalog.Title(r.Context(), kind, id); err == nil {
			item.Title = named.Title
		}
	}
	// The write has its own deadline, whatever the lookup took.
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	defer cancel()
	if err := feedback.SetFeedback(ctx, uid, which, store.TitleRef{Kind: kind, TMDBID: id, Title: item.Title}, on); err != nil {
		slog.Warn("saving recommendation feedback failed", "error", logError(err))
		http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
		return
	}
	var undone *tmdb.MediaItem
	if which == store.NotInterested && on {
		undone = &item
	}
	rc.patchRows(w, r, uid, undone)
}

// localizeSeeds names the seeds rows can start from in the interface
// language, for their rows' headings.
func (rc *Recommendations) localizeSeeds(ctx context.Context, seeds []seed) []seed {
	seeds = seeds[:min(len(seeds), recommendationSeeds)] // only these make rows
	items := make([]tmdb.MediaItem, len(seeds))
	for i, s := range seeds {
		items[i] = tmdb.MediaItem{ID: s.id, MediaType: s.kind, Title: s.title}
	}
	named := rc.catalog.LocalizeTitles(ctx, items)
	out := slices.Clone(seeds)
	for i := range out {
		out[i].title = named[i].Title
	}
	return out
}
