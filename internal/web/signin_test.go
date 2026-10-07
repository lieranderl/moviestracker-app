package web_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/sources"
	"github.com/lieranderl/moviestracker-app/internal/web"
)

func TestTheSignInPageExplainsHowMoviestrackerWorks(t *testing.T) {
	body := get(t, web.New(web.Config{}), "/").Body.String()
	for _, step := range []string{"Browse what is trending", "Get its sources", "Add one to your TorrServer"} {
		if !strings.Contains(body, step) {
			t.Errorf("sign-in page lacks the step %q", step)
		}
	}
}

func TestTheSignInPageExplainsHowMoviestrackerWorksInRussian(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept-Language", "ru")
	web.New(web.Config{}).ServeHTTP(rec, req)
	if body := rec.Body.String(); !strings.Contains(body, "Найдите источники") {
		t.Errorf("Russian sign-in page lacks the step %q", "Найдите источники")
	}
}

func TestTheSignInPageShowsThisWeeksTrendingPosters(t *testing.T) {
	// A stand-in for TMDB (the external boundary) trending one movie and one series.
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/3/trending/movie/week":
			_, _ = w.Write([]byte(`{"page":1,"total_pages":1,"results":[{"id":438631,"title":"Dune","poster_path":"/dune.jpg","release_date":"2021-09-15"}]}`))
		case "/3/trending/tv/week":
			_, _ = w.Write([]byte(`{"page":1,"total_pages":1,"results":[{"id":95396,"name":"Severance","poster_path":"/severance.jpg","first_air_date":"2022-02-17"}]}`))
		default:
			_, _ = w.Write([]byte(`{"page":1,"total_pages":1,"results":[]}`))
		}
	}))
	t.Cleanup(fake.Close)
	cfg := web.Config{Sources: sources.Connector{TMDBBaseURL: fake.URL}.Connect(config.Sources{TMDBKey: "tmdb-key"})}

	body := get(t, web.New(cfg), "/").Body.String()
	for _, poster := range []string{"https://image.tmdb.org/t/p/w500/dune.jpg", "https://image.tmdb.org/t/p/w500/severance.jpg"} {
		if !strings.Contains(body, poster) {
			t.Errorf("sign-in page lacks the trending poster %s", poster)
		}
	}
}
