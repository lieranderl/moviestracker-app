package web_test

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/sources"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/web"
)

func localizedWeb(t *testing.T, namingFails bool) (http.Handler, *httptest.ResponseRecorder, *store.Memory) {
	t.Helper()
	g := newGoogle(t)
	cfg := g.config()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/3/movie/438631" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if namingFails && r.URL.Query().Get("append_to_response") == "" {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.URL.Query().Get("language") == "ru-RU" {
			_, _ = w.Write([]byte(`{"id":438631,"title":"Дюна","poster_path":"/d-ru.jpg","recommendations":{"results":[{"id":329865,"title":"Прибытие"}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":438631,"title":"Dune","poster_path":"/d.jpg"}`))
	}))
	t.Cleanup(provider.Close)
	cfg.Sources = sources.Connector{TMDBBaseURL: provider.URL}.Connect(config.Sources{TMDBKey: "test-key"})
	users := store.NewMemory()
	cfg.Store = users
	h := web.New(cfg)
	session := signIn(t, h)
	if err := users.SetTitleState(context.Background(), "1098765", store.TitleState{
		Kind: "movie", TMDBID: 438631, Title: "Dune", State: store.Watched,
	}); err != nil {
		t.Fatal(err)
	}
	return h, session, users
}

func russianWebPage(t *testing.T, h http.Handler, session *httptest.ResponseRecorder, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Accept-Language", "ru-RU")
	for _, c := range session.Result().Cookies() {
		if c.MaxAge >= 0 {
			req.AddCookie(c)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAWebUserFindsASavedTitleInTheInterfaceLanguage(t *testing.T) {
	h, session, _ := localizedWeb(t, false)
	rec := russianWebPage(t, h, session, "/lists/watched?q="+url.QueryEscape("Дюна"))
	body := html.UnescapeString(rec.Body.String())
	if rec.Code != http.StatusOK || !strings.Contains(body, `href="/movie/438631"`) || !strings.Contains(body, "Дюна") {
		t.Fatalf("Russian library search = %d; want the saved Dune title named Дюна:\n%s", rec.Code, body)
	}
}

func TestWebFavouritesListsAndRecommendationsKeepLocalizedNames(t *testing.T) {
	h, session, users := localizedWeb(t, false)
	send(t, h, http.MethodPost, "/api/favorites?type=movie&id=438631", session)
	sendSignals(t, h, http.MethodPost, "/api/lists?type=movie&id=438631", `{"listName":"Sci-fi"}`, session)
	lists, err := users.CustomLists(context.Background(), "1098765")
	if err != nil || len(lists) != 1 {
		t.Fatalf("custom lists = %v, %v; want one list", lists, err)
	}
	for _, path := range []string{"/favorites", "/lists/c/" + lists[0].ID, "/api/recommendations"} {
		rec := russianWebPage(t, h, session, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "Дюна") {
			t.Errorf("GET %s lost the localized title", path)
		}
	}
}

func TestWebLibraryKeepsASavedNameWhenNamingFails(t *testing.T) {
	h, session, _ := localizedWeb(t, true)
	rec := russianWebPage(t, h, session, "/lists/watched?q=Dune")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `href="/movie/438631"`) {
		t.Errorf("library with naming unavailable = %d; want the saved Dune title", rec.Code)
	}
}

// detailsWithoutNames is an external provider without optional title naming.
type detailsWithoutNames struct{ tmdb.DetailsProvider }

func TestWebLibraryKeepsASavedNameWithoutOptionalNaming(t *testing.T) {
	g := newGoogle(t)
	cfg := withTMDB(t, g.config())
	cfg.Sources.Details = detailsWithoutNames{cfg.Sources.Details}
	users := store.NewMemory()
	cfg.Store = users
	h := web.New(cfg)
	session := signIn(t, h)
	if err := users.SetTitleState(context.Background(), "1098765", store.TitleState{
		Kind: "movie", TMDBID: 438631, Title: "Dune", State: store.Watched,
	}); err != nil {
		t.Fatal(err)
	}
	rec := russianWebPage(t, h, session, "/lists/watched?q=Dune")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `href="/movie/438631"`) {
		t.Errorf("library without optional naming = %d; want the saved Dune title", rec.Code)
	}
}
