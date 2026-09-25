package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/auth"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

type mockTrendingProvider struct {
	hero   []tmdb.MediaItem
	movies []tmdb.MediaItem
	series []tmdb.MediaItem
	err    error
	calls  int
}

func (m *mockTrendingProvider) GetCatalog(context.Context) (tmdb.Catalog, error) {
	m.calls++
	return tmdb.Catalog{Hero: m.hero, Movies: m.movies, Series: m.series}, m.err
}

type blockingCatalogProvider struct {
	observedErr error
}

func (b *blockingCatalogProvider) GetCatalog(ctx context.Context) (tmdb.Catalog, error) {
	select {
	case <-ctx.Done():
		b.observedErr = ctx.Err()
		return tmdb.Catalog{}, ctx.Err()
	case <-time.After(100 * time.Millisecond):
		return tmdb.Catalog{}, nil
	}
}

func TestMoviesPageUnauthenticatedRedirectsToLogin(t *testing.T) {
	server := newTestServer(t)

	req := httptest.NewRequest(http.MethodGet, "/movies", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected status 303 See Other, got %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Fatalf("expected redirect Location /login, got %q", loc)
	}
}

func TestMoviesPageAuthenticatedRendersDashboard(t *testing.T) {
	testUser := &auth.User{
		Username: "alex",
		Name:     "Alex Johnson",
		Role:     "user",
	}

	mockTP := &mockTrendingProvider{
		hero: []tmdb.MediaItem{
			{
				ID:           1,
				Title:        "Cosmic Horizons",
				Overview:     "Deep space exploration",
				BackdropPath: "/cosmic_bg.jpg",
				LogoPath:     "/cosmic_logo.png",
				VoteAverage:  8.9,
				VoteCount:    15400,
				MediaType:    "movie",
			},
		},
		movies: []tmdb.MediaItem{
			{
				ID:          1,
				Title:       "Cosmic Horizons",
				Overview:    "Deep space exploration",
				PosterPath:  "/cosmic.jpg",
				VoteAverage: 8.9,
				VoteCount:   15400,
				ReleaseDate: "2026-03-01",
				MediaType:   "movie",
			},
		},
		series: []tmdb.MediaItem{
			{
				ID:          2,
				Title:       "Cyberpunk Legacy",
				Overview:    "Futuristic thriller",
				PosterPath:  "/cyber.jpg",
				VoteAverage: 9.2,
				VoteCount:   8200,
				ReleaseDate: "2026-04-01",
				MediaType:   "tv",
			},
		},
	}

	cfg := validTestConfig(t)
	cfg.TMDB = mockTP
	server := newTestServerWithConfig(t, cfg)

	token, err := server.sessions.CreateSession(testUser)
	if err != nil {
		t.Fatalf("CreateSession(): %v", err)
	}
	cookie := &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}

	req := httptest.NewRequest(http.MethodGet, "/movies", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d", rec.Code)
	}
	if mockTP.calls != 1 {
		t.Fatalf("catalog calls = %d, want 1", mockTP.calls)
	}

	tbody := rec.Body.String()
	for _, expected := range []string{
		"Trending Movies",
		"Trending TV Series",
		"Cosmic Horizons",
		"Cyberpunk Legacy",
		"carousel",
		"cosmic_logo.png",
		"cosmic_bg.jpg",
		"TMDB: 8.9/10 (15k votes)",
		"(15k)",
	} {
		if !strings.Contains(tbody, expected) {
			t.Errorf("rendered body missing %q", expected)
		}
	}
}

func TestMoviesBoundsCatalogRetrieval(t *testing.T) {
	testUser := &auth.User{
		Username: "alex",
		Name:     "Alex Johnson",
		Role:     "user",
	}

	provider := &blockingCatalogProvider{}
	cfg := validTestConfig(t)
	cfg.CatalogTimeout = 20 * time.Millisecond
	cfg.TMDB = provider
	server := newTestServerWithConfig(t, cfg)

	token, err := server.sessions.CreateSession(testUser)
	if err != nil {
		t.Fatalf("CreateSession(): %v", err)
	}
	cookie := &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}

	start := time.Now()
	req := httptest.NewRequest(http.MethodGet, "/movies", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if time.Since(start) > 500*time.Millisecond {
		t.Fatalf("handler took %v, want prompt return under deadline", time.Since(start))
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !errors.Is(provider.observedErr, context.DeadlineExceeded) {
		t.Fatalf("provider observed error = %v, want %v", provider.observedErr, context.DeadlineExceeded)
	}
	tbody := rec.Body.String()
	if !strings.Contains(tbody, "Trending Movies") {
		t.Errorf("rendered body missing empty-state trending section: %q", tbody)
	}
}

func TestTrendingHeroLinksToTitlePages(t *testing.T) {
	cfg := validTestConfig(t)
	cfg.TMDB = &mockTrendingProvider{hero: []tmdb.MediaItem{
		{ID: 27205, Title: "Inception", MediaType: "movie", BackdropPath: "/b.jpg"},
		{ID: 1396, Title: "Breaking Bad", MediaType: "tv", BackdropPath: "/c.jpg"},
	}}
	server := newTestServerWithConfig(t, cfg)

	body := get(t, server, "/movies", true).Body.String()

	for _, want := range []string{`href="/movie/27205"`, `href="/tv/1396"`, "More info"} {
		if !strings.Contains(body, want) {
			t.Errorf("trending hero missing %q", want)
		}
	}
}
