package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/imdb"
)

type mockIMDbProvider struct {
	rating *imdb.Rating
	err    error
}

func (m *mockIMDbProvider) GetRating(ctx context.Context, id string) (*imdb.Rating, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.rating, nil
}

func TestMovieIMDbRating_Success(t *testing.T) {
	cfg := validTestConfig(t)
	cfg.IMDb = &mockIMDbProvider{
		rating: &imdb.Rating{
			Rating: "7.8",
			Votes:  "120k",
		},
	}
	server := newTestServerWithConfig(t, cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/movies/imdb-rating?id=tt27165187&target=hero-imdb-rating-0", nil)
	req.AddCookie(signedIn(t, server))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "hero-imdb-rating-0") {
		t.Errorf("expected target hero-imdb-rating-0 in response, got: %s", body)
	}
	if !strings.Contains(body, "7.8") {
		t.Errorf("expected rating 7.8 in response, got: %s", body)
	}
	if !strings.Contains(body, "120k") {
		t.Errorf("expected votes 120k in response, got: %s", body)
	}
	if !strings.Contains(body, "datastar-patch-elements") {
		t.Errorf("expected datastar-patch-elements event, got: %s", body)
	}
}

func TestMovieIMDbRating_MissingID(t *testing.T) {
	cfg := validTestConfig(t)
	cfg.IMDb = &mockIMDbProvider{
		rating: &imdb.Rating{Rating: "8.0"},
	}
	server := newTestServerWithConfig(t, cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/movies/imdb-rating?id=&target=hero-imdb-rating-0", nil)
	req.AddCookie(signedIn(t, server))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 Bad Request for empty ID, got %d", rec.Code)
	}
}

func TestMovieIMDbRating_NotConfigured(t *testing.T) {
	cfg := validTestConfig(t)
	cfg.IMDb = nil
	server := newTestServerWithConfig(t, cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/movies/imdb-rating?id=tt27165187", nil)
	req.AddCookie(signedIn(t, server))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found when IMDb client not configured, got %d", rec.Code)
	}
}

func TestMovieIMDbRating_UpstreamError(t *testing.T) {
	cfg := validTestConfig(t)
	cfg.IMDb = &mockIMDbProvider{
		err: errors.New("cloud run service down"),
	}
	server := newTestServerWithConfig(t, cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/movies/imdb-rating?id=tt27165187&target=hero-imdb-rating-0", nil)
	req.AddCookie(signedIn(t, server))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	// Should return 200 with clean fragment clearing the loading spinner
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 with empty fragment on error, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "hero-imdb-rating-0") {
		t.Errorf("expected target hero-imdb-rating-0 in response, got: %s", body)
	}
	// Should not have rating
	if strings.Contains(body, "7.8") {
		t.Errorf("expected no rating in error response, got: %s", body)
	}
}

func TestMovieIMDbRating_FormatThousands(t *testing.T) {
	cfg := validTestConfig(t)
	cfg.IMDb = &mockIMDbProvider{
		rating: &imdb.Rating{
			Rating: "7.8",
			Votes:  "2000",
		},
	}
	server := newTestServerWithConfig(t, cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/movies/imdb-rating?id=tt27165187&target=hero-imdb-rating-0", nil)
	req.AddCookie(signedIn(t, server))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "(2k)") {
		t.Errorf("expected (2k) votes format in response, got: %s", body)
	}
	if !strings.Contains(body, `IMDb: 7.8/10 (2k votes)`) {
		t.Errorf("expected title with (2k votes), got: %s", body)
	}
}

func TestMovieIMDbRating_FormatRawVotes(t *testing.T) {
	cfg := validTestConfig(t)
	cfg.IMDb = &mockIMDbProvider{
		rating: &imdb.Rating{
			Rating: "6.3",
			Votes:  "56844",
		},
	}
	server := newTestServerWithConfig(t, cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/movies/imdb-rating?id=tt27165187&target=hero-imdb-rating-0", nil)
	req.AddCookie(signedIn(t, server))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "(57k)") {
		t.Errorf("expected (57k) votes format in response, got: %s", body)
	}
	if !strings.Contains(body, `IMDb: 6.3/10 (57k votes)`) {
		t.Errorf("expected title with (57k votes), got: %s", body)
	}
}

func TestMovieIMDbRating_RequiresSignIn(t *testing.T) {
	cfg := validTestConfig(t)
	cfg.IMDb = &mockIMDbProvider{rating: &imdb.Rating{Rating: "8.0"}}
	server := newTestServerWithConfig(t, cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/movies/imdb-rating?id=tt27165187", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestMovieIMDbRating_OneRequestFillsEveryHeroBadge(t *testing.T) {
	cfg := validTestConfig(t)
	cfg.IMDb = &mockIMDbProvider{rating: &imdb.Rating{Rating: "7.8", Votes: "120000"}}
	server := newTestServerWithConfig(t, cfg)

	req := httptest.NewRequest(http.MethodGet, "/api/movies/imdb-rating?id=tt0000001&target=hero-imdb-rating-0&id=tt0000002&target=hero-imdb-rating-3", nil)
	req.AddCookie(signedIn(t, server))
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	body := rec.Body.String()
	for _, target := range []string{`id="hero-imdb-rating-0"`, `id="hero-imdb-rating-3"`} {
		if !strings.Contains(body, target) {
			t.Errorf("missing patch for %s in:\n%s", target, body)
		}
	}
	if n := strings.Count(body, "event: datastar-patch-elements"); n != 2 {
		t.Errorf("patches = %d, want 2", n)
	}
}
