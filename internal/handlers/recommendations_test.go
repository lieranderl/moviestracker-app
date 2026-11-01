package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

func TestTheLocalHomePageRecommendsAtMostThreeRowsWithoutRepeats(t *testing.T) {
	related := func(ids ...int) []tmdb.MediaItem {
		var out []tmdb.MediaItem
		for _, id := range ids {
			out = append(out, tmdb.MediaItem{ID: id, Title: "Title", MediaType: "movie"})
		}
		return out
	}
	movies := map[int]*tmdb.MovieDetails{}
	for id, recs := range map[int][]int{4: {11, 12}, 3: {12, 13}, 2: {14}, 1: {15}} {
		movies[id] = &tmdb.MovieDetails{MediaItem: tmdb.MediaItem{ID: id, Title: "Seed", MediaType: "movie"}, Recommendations: related(recs...)}
	}
	s := newMediaServer(t, &fakeTMDBDetails{movies: movies}, nil, "")
	for _, id := range []string{"1", "2", "3", "4"} {
		sendAs(t, s, http.MethodPost, "/api/titles/state?type=movie&id="+id+"&state=watched")
	}
	body := sendAs(t, s, http.MethodGet, "/api/recommendations")
	if got := strings.Count(body, "Because you watched"); got != 3 {
		t.Errorf("%d rows, want 3", got)
	}
	if got := strings.Count(body, `href="/movie/12"`); got != 1 {
		t.Errorf("movie 12 shows %d times, want once", got)
	}
}

// slowTMDB is TMDB answering recommendation lookups slowly or not at all,
// once on: a held movie answers when its channel closes (or never, if that
// never happens before the request ends), a failing one errors. Answering
// a movie closes answered[id].
type slowTMDB struct {
	*fakeTMDBDetails
	on       atomic.Bool
	hold     map[int]chan struct{}
	fail     map[int]bool
	answered map[int]chan struct{}

	mu                    sync.Mutex
	inFlight, maxInFlight int
}

func (f *slowTMDB) Movie(ctx context.Context, id int) (*tmdb.MovieDetails, error) {
	if !f.on.Load() {
		return f.fakeTMDBDetails.Movie(ctx, id)
	}
	f.mu.Lock()
	f.inFlight++
	f.maxInFlight = max(f.maxInFlight, f.inFlight)
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.inFlight--
		f.mu.Unlock()
	}()
	if ch, ok := f.hold[id]; ok {
		select {
		case <-ch:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.fail[id] {
		return nil, errors.New("tmdb is down")
	}
	defer func() {
		if ch, ok := f.answered[id]; ok {
			close(ch)
		}
	}()
	return f.fakeTMDBDetails.Movie(ctx, id)
}

// slowRecommendations is a server whose user watched movies 1 to 6, in
// that order (so 6 is the newest seed), each recommending its own movie
// (id*10), with lookups held to budget.
func slowRecommendations(t *testing.T, budget time.Duration, f *slowTMDB) *Server {
	t.Helper()
	f.fakeTMDBDetails = &fakeTMDBDetails{movies: map[int]*tmdb.MovieDetails{}}
	for id := 1; id <= 6; id++ {
		f.movies[id] = &tmdb.MovieDetails{
			MediaItem:       tmdb.MediaItem{ID: id, Title: fmt.Sprint("Seed ", id), MediaType: "movie"},
			Recommendations: []tmdb.MediaItem{{ID: id * 10, Title: "Like it", MediaType: "movie"}},
		}
	}
	cfg := validTestConfig(t)
	cfg.Details = f
	cfg.CatalogTimeout = budget
	s := newTestServerWithConfig(t, cfg)
	for id := 1; id <= 6; id++ {
		sendAs(t, s, http.MethodPost, fmt.Sprintf("/api/titles/state?type=movie&id=%d&state=watched", id))
	}
	f.on.Store(true)
	return s
}

// rowTitles are the seeds of the recommendation rows, top to bottom.
func rowTitles(body string) []string {
	var out []string
	for _, m := range regexp.MustCompile(`Because you watched (Seed \d)`).FindAllStringSubmatch(body, -1) {
		out = append(out, m[1])
	}
	return out
}

// Titles TMDB never answers for cost the rows one deadline, not one each,
// and the titles it answered for still make rows.
func TestSlowTitlesDelayRecommendationsByOneDeadlineAndKeepTheRest(t *testing.T) {
	const budget = 200 * time.Millisecond
	never := make(chan struct{})
	defer close(never)
	f := &slowTMDB{hold: map[int]chan struct{}{6: never, 5: never, 4: never}, fail: map[int]bool{1: true}}
	s := slowRecommendations(t, budget, f)
	start := time.Now()
	body := sendAs(t, s, http.MethodGet, "/api/recommendations")
	if took := time.Since(start); took > 2*budget {
		t.Errorf("recommendations took %s with three titles TMDB never answered; want within %s", took, 2*budget)
	}
	if got := rowTitles(body); !slices.Equal(got, []string{"Seed 3", "Seed 2"}) {
		t.Errorf("rows = %v, want the answered Seed 3 then Seed 2", got)
	}
	if f.maxInFlight > 3 {
		t.Errorf("%d TMDB lookups at once, want at most 3", f.maxInFlight)
	}
}

// Rows keep the order of the titles they start from, whichever TMDB
// answers first.
func TestRecommendationRowsKeepTheirOrderWhicheverAnswersFirst(t *testing.T) {
	five := make(chan struct{})
	f := &slowTMDB{hold: map[int]chan struct{}{6: five}, answered: map[int]chan struct{}{5: five}}
	s := slowRecommendations(t, time.Second, f)
	body := sendAs(t, s, http.MethodGet, "/api/recommendations")
	if got := rowTitles(body); !slices.Equal(got, []string{"Seed 6", "Seed 5", "Seed 4"}) {
		t.Errorf("rows = %v, want Seed 6, 5, 4 though 5 answered before 6", got)
	}
}
