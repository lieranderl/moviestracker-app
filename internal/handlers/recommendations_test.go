package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

// Home recommends one row, mixing TMDB's picks for the three titles the
// user watched last: a title from each in turn, none twice.
func TestTheLocalHomePageMixesTheLastThreeWatchedIntoOneRow(t *testing.T) {
	related := func(ids ...int) []tmdb.MediaItem {
		var out []tmdb.MediaItem
		for _, id := range ids {
			out = append(out, tmdb.MediaItem{ID: id, Title: "Title", MediaType: "movie"})
		}
		return out
	}
	movies := map[int]*tmdb.MovieDetails{}
	for id, recs := range map[int][]int{4: {11, 12}, 3: {12, 13}, 2: {14}, 1: {15}} {
		movies[id] = &tmdb.MovieDetails{MediaItem: tmdb.MediaItem{ID: id, Title: fmt.Sprint("Seed ", id), MediaType: "movie"}, Recommendations: related(recs...)}
	}
	s := newMediaServer(t, &fakeTMDBDetails{movies: movies}, nil, "")
	for _, id := range []string{"1", "2", "3", "4"} {
		sendAs(t, s, http.MethodPost, "/api/titles/state?type=movie&id="+id+"&state=watched")
	}
	body := sendAs(t, s, http.MethodGet, "/api/recommendations")
	if got := strings.Count(body, "<h2"); got != 1 {
		t.Errorf("%d rows, want 1", got)
	}
	if !strings.Contains(body, "Because you watched Seed 4, Seed 3 and Seed 2") {
		t.Errorf("the row does not name the three titles it is made of:\n%s", body)
	}
	var got []string
	for _, m := range regexp.MustCompile(`href="/movie/(\d+)"`).FindAllStringSubmatch(body, -1) {
		got = append(got, m[1])
	}
	if !slices.Equal(got, []string{"11", "12", "14", "13"}) {
		t.Errorf("row = %v, want 11, 12, 14, 13: one from each title in turn, none twice, nothing from the fourth", got)
	}
}

// The row follows what the user watched last, a series as much as a
// movie: a title rated highly long ago does not push out this week's. (On a
// coarse clock the marks can share a moment; ties order by kind and id, which
// here agree with the order watched.)
func TestTheRowFollowsTheLatestWatchedNotTheBestRated(t *testing.T) {
	one := func(id int, kind string) []tmdb.MediaItem {
		return []tmdb.MediaItem{{ID: id, Title: "Title", MediaType: kind}}
	}
	movies := map[int]*tmdb.MovieDetails{}
	for id, rec := range map[int]int{1: 11, 2: 12, 3: 13} {
		movies[id] = &tmdb.MovieDetails{MediaItem: tmdb.MediaItem{ID: id, Title: fmt.Sprint("Film ", id), MediaType: "movie"}, Recommendations: one(rec, "movie")}
	}
	series := map[int]*tmdb.TVDetails{4: {MediaItem: tmdb.MediaItem{ID: 4, Title: "Show 4", MediaType: "tv"}, Recommendations: one(14, "tv")}}
	s := newMediaServer(t, &fakeTMDBDetails{movies: movies, series: series}, nil, "")
	cookie := signedIn(t, s)
	send := func(path, body string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Datastar-Request", "true")
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST %s = %d %s", path, rec.Code, rec.Body)
		}
	}
	send("/api/titles/state?type=movie&id=1&state=watched", "")
	send("/api/titles/review?type=movie&id=1", `{"reviewRating":9}`)
	send("/api/titles/state?type=movie&id=2&state=watched", "")
	send("/api/titles/state?type=movie&id=3&state=watched", "")
	send("/api/titles/state?type=tv&id=4&state=watched", "")

	body := sendAs(t, s, http.MethodGet, "/api/recommendations")
	if !strings.Contains(body, "Because you watched Show 4, Film 3 and Film 2") {
		t.Errorf("the row is not made of the three titles watched last:\n%s", body)
	}
	if !strings.Contains(body, `href="/tv/14"`) || strings.Contains(body, `href="/movie/11"`) {
		t.Error("the row should mix in the series' picks and leave out the older, best-rated film's")
	}
}

// The row holds as many titles as a catalog rail, and names a row made
// partly of saved titles for what it is based on.
func TestTheRowHoldsTwentyTitlesAndSaysWhatItIsBasedOn(t *testing.T) {
	movies := map[int]*tmdb.MovieDetails{}
	for id := 1; id <= 3; id++ {
		var recs []tmdb.MediaItem
		for n := range 10 {
			recs = append(recs, tmdb.MediaItem{ID: id*100 + n, Title: "Title", MediaType: "movie"})
		}
		movies[id] = &tmdb.MovieDetails{MediaItem: tmdb.MediaItem{ID: id, Title: fmt.Sprint("Film ", id), MediaType: "movie"}, Recommendations: recs}
	}
	s := newMediaServer(t, &fakeTMDBDetails{movies: movies}, nil, "")
	sendAs(t, s, http.MethodPost, "/api/titles/state?type=movie&id=1&state=watched")
	sendAs(t, s, http.MethodPost, "/api/titles/state?type=movie&id=2&state=watched")
	sendAs(t, s, http.MethodPost, "/api/titles/state?type=movie&id=3&state=want")

	body := sendAs(t, s, http.MethodGet, "/api/recommendations")
	if got := regexp.MustCompile(`href="/movie/\d+"`).FindAllString(body, -1); len(got) != 20 {
		t.Errorf("row has %d titles, want 20", len(got))
	}
	if !strings.Contains(body, "Based on Film 2, Film 1 and Film 3") {
		t.Errorf("a row partly of saved titles should say what it is based on:\n%s", body)
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

// rowTitles are the titles the recommendation row is made of, as its
// heading names them.
func rowTitles(body string) []string {
	m := regexp.MustCompile(`Because you watched ([^<]+)`).FindStringSubmatch(body)
	if m == nil {
		return nil
	}
	return regexp.MustCompile(`, | and `).Split(strings.TrimSpace(m[1]), -1)
}

// Titles TMDB never answers for cost the rows one deadline, not one each,
// and the titles it answered for still make the row.
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
		t.Errorf("row is made of %v, want the answered Seed 3 then Seed 2", got)
	}
	if f.maxInFlight > 3 {
		t.Errorf("%d TMDB lookups at once, want at most 3", f.maxInFlight)
	}
}

// The row keeps the order of the titles it is made of, whichever TMDB
// answers first.
func TestRecommendationRowsKeepTheirOrderWhicheverAnswersFirst(t *testing.T) {
	five := make(chan struct{})
	f := &slowTMDB{hold: map[int]chan struct{}{6: five}, answered: map[int]chan struct{}{5: five}}
	s := slowRecommendations(t, time.Second, f)
	body := sendAs(t, s, http.MethodGet, "/api/recommendations")
	if got := rowTitles(body); !slices.Equal(got, []string{"Seed 6", "Seed 5", "Seed 4"}) {
		t.Errorf("row is made of %v, want Seed 6, 5, 4 though 5 answered before 6", got)
	}
}
