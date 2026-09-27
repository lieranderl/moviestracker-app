package tmdb

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
)

// multilingualTMDB answers in the language each request asks for, as TMDB
// does, and counts requests.
func multilingualTMDB(t *testing.T, bodies map[string]map[string]string) (*Client, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		lang := r.URL.Query().Get("language")
		body, ok := bodies[r.URL.Path][lang]
		if !ok {
			http.Error(w, fmt.Sprintf(`{"status_message":"no %s body for %s"}`, lang, r.URL.Path), http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return NewClient("test-key", WithBaseURL(server.URL), WithHTTPClient(server.Client())), &hits
}

func TestTitlesComeInTheLanguageTheRequestCarries(t *testing.T) {
	client, hits := multilingualTMDB(t, map[string]map[string]string{
		"/3/movie/27205": {
			"en-US": `{"id":27205,"title":"Inception","overview":"Cobb steals secrets.","genres":[{"id":28,"name":"Action"}]}`,
			"ru-RU": `{"id":27205,"title":"Начало","overview":"Кобб крадёт секреты.","genres":[{"id":28,"name":"боевик"}]}`,
		},
	})
	ru := i18n.WithLang(context.Background(), i18n.Russian)

	for range 2 {
		en, err := client.Movie(context.Background(), 27205)
		if err != nil {
			t.Fatalf("English Movie(): %v", err)
		}
		if en.Title != "Inception" || en.Genres[0].Name != "Action" {
			t.Errorf("English = %q / %q", en.Title, en.Genres[0].Name)
		}
		got, err := client.Movie(ru, 27205)
		if err != nil {
			t.Fatalf("Russian Movie(): %v", err)
		}
		if got.Title != "Начало" || got.Overview != "Кобб крадёт секреты." || got.Genres[0].Name != "боевик" {
			t.Errorf("Russian = %q / %q / %q", got.Title, got.Overview, got.Genres[0].Name)
		}
	}
	if hits.Load() != 2 {
		t.Errorf("requests = %d, want one per language (each cached)", hits.Load())
	}
}

func TestAnUntranslatedOverviewFallsBackToEnglish(t *testing.T) {
	client, _ := multilingualTMDB(t, map[string]map[string]string{
		"/3/movie/1": {"ru-RU": `{"id":1,"title":"Фильм","overview":"","tagline":"",
			"translations":{"translations":[
				{"iso_639_1":"de","data":{"overview":"Ein Film.","tagline":"Los!"}},
				{"iso_639_1":"en","data":{"overview":"A film.","tagline":"Go!"}}]}}`},
		"/3/tv/2": {"ru-RU": `{"id":2,"name":"Сериал","overview":"",
			"translations":{"translations":[{"iso_639_1":"en","data":{"overview":"A show.","tagline":"Watch."}}]}}`},
		"/3/person/3": {"ru-RU": `{"id":3,"name":"Анна","biography":"",
			"translations":{"translations":[{"iso_639_1":"en","data":{"biography":"An actor."}}]}}`},
	})
	ru := i18n.WithLang(context.Background(), i18n.Russian)

	movie, err := client.Movie(ru, 1)
	if err != nil {
		t.Fatalf("Movie(): %v", err)
	}
	if movie.Overview != "A film." || movie.Tagline != "Go!" {
		t.Errorf("movie overview/tagline = %q / %q", movie.Overview, movie.Tagline)
	}
	show, err := client.TV(ru, 2)
	if err != nil {
		t.Fatalf("TV(): %v", err)
	}
	if show.Overview != "A show." || show.Tagline != "Watch." {
		t.Errorf("show overview/tagline = %q / %q", show.Overview, show.Tagline)
	}
	person, err := client.Person(ru, 3)
	if err != nil {
		t.Fatalf("Person(): %v", err)
	}
	if person.Biography != "An actor." {
		t.Errorf("biography = %q", person.Biography)
	}
}

func TestRussianPagesPreferRussianLogosAndTrailers(t *testing.T) {
	var asked atomic.Value
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Store(r.URL.Query())
		_, _ = w.Write([]byte(`{"id":1,"title":"Фильм",
			"images":{"logos":[{"file_path":"/en.png","iso_639_1":"en"},{"file_path":"/ru.png","iso_639_1":"ru"}]},
			"videos":{"results":[
				{"key":"enTrailer01","site":"YouTube","type":"Trailer","official":true,"iso_639_1":"en"},
				{"key":"ruTrailer01","site":"YouTube","type":"Trailer","official":true,"iso_639_1":"ru"}]}}`))
	}))
	t.Cleanup(server.Close)
	client := NewClient("test-key", WithBaseURL(server.URL), WithHTTPClient(server.Client()))

	m, err := client.Movie(i18n.WithLang(context.Background(), i18n.Russian), 1)
	if err != nil {
		t.Fatalf("Movie(): %v", err)
	}
	if m.LogoPath != "/ru.png" || m.TrailerKey != "ruTrailer01" {
		t.Errorf("logo, trailer = %q, %q", m.LogoPath, m.TrailerKey)
	}
	if len(m.Videos) != 2 || m.Videos[0].Key != "ruTrailer01" {
		t.Errorf("videos = %+v, want the Russian trailer first", m.Videos)
	}
	q := asked.Load().(url.Values)
	for param, want := range map[string]string{"include_image_language": "ru,en,null", "include_video_language": "ru,en,null"} {
		if got := strings.Join(q[param], ","); got != want {
			t.Errorf("%s = %q, want %q", param, got, want)
		}
	}
}

func TestEachLanguageHasItsOwnHomeCatalog(t *testing.T) {
	trending := func(title string) string {
		return `{"results":[{"id":1,"title":"` + title + `","backdrop_path":"/b.jpg","media_type":"movie"}]}`
	}
	details := `{"images":{"logos":[]},"videos":{"results":[]}}`
	client, hits := multilingualTMDB(t, map[string]map[string]string{
		"/3/trending/movie/week": {"en-US": trending("Dune"), "ru-RU": trending("Дюна")},
		"/3/trending/tv/week":    {"en-US": `{"results":[]}`, "ru-RU": `{"results":[]}`},
		"/3/movie/1":             {"en-US": details, "ru-RU": details},
	})
	ru := i18n.WithLang(context.Background(), i18n.Russian)

	for range 2 {
		en, err := client.GetCatalog(context.Background())
		if err != nil || len(en.Movies) != 1 || en.Movies[0].Title != "Dune" {
			t.Fatalf("English catalog = %+v, %v", en.Movies, err)
		}
		got, err := client.GetCatalog(ru)
		if err != nil || len(got.Movies) != 1 || got.Movies[0].Title != "Дюна" {
			t.Fatalf("Russian catalog = %+v, %v", got.Movies, err)
		}
	}
	if hits.Load() != 6 {
		t.Errorf("requests = %d, want 3 per language", hits.Load())
	}
}
