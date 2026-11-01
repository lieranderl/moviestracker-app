package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// Recommendation limits: rows on the home page, and titles whose TMDB
// recommendations are looked up to fill them.
const (
	recommendationRows  = 3
	recommendationSeeds = 6
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

// handleRows patches the home page's recommendation rows: one per title
// the user watched or saved, holding TMDB's recommendations for it that the
// user has not marked and no row above shows.
func (rc *Recommendations) handleRows(w http.ResponseWriter, r *http.Request) {
	uid, ok := rc.user(r)
	if !ok {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), progressTimeout)
	seeds, shown, err := rc.seeds(ctx, uid)
	cancel()
	if err != nil {
		slog.Warn("reading what to recommend from failed", "error", logError(err))
	}
	var rows []views.RecommendationRow
	for i, s := range seeds {
		if len(rows) == recommendationRows || i == recommendationSeeds {
			break
		}
		related, err := rc.catalog.related(r.Context(), s.kind, s.id)
		if err != nil {
			slog.Warn("tmdb recommendations failed", "kind", s.kind, "id", s.id, "error", logError(err))
			continue
		}
		row := views.RecommendationRow{Title: i18n.Tf(r.Context(), "Because you saved %s", s.title)}
		if s.watched {
			row.Title = i18n.Tf(r.Context(), "Because you watched %s", s.title)
		}
		for _, it := range related {
			if key := titleKey(it.MediaType, it.ID); !shown[key] {
				shown[key] = true
				row.Items = append(row.Items, it)
			}
		}
		if len(row.Items) > 0 {
			rows = append(rows, row)
		}
	}
	w.Header().Set("Cache-Control", "no-store")
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.RecommendationRows(rows)); err != nil {
		slog.Warn("patching recommendations failed", "error", err)
	}
}
