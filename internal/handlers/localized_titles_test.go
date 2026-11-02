package handlers

import (
	"errors"
	"html"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

// localizedServer is a local app whose user watched Dune, saved under its
// English name; TMDB names it Дюна in Russian.
func localizedServer(t *testing.T, titleErr error) *Server {
	t.Helper()
	details := &fakeTMDBDetails{
		movies: map[int]*tmdb.MovieDetails{693134: {MediaItem: tmdb.MediaItem{ID: 693134, Title: "Dune: Part Two", MediaType: "movie"}}, 438631: {
			MediaItem:       tmdb.MediaItem{ID: 438631, Title: "Dune", MediaType: "movie", PosterPath: "/d.jpg"},
			Recommendations: []tmdb.MediaItem{{ID: 693134, Title: "Dune: Part Two", MediaType: "movie"}},
		}},
		titleIn: map[i18n.Lang]map[string]tmdb.MediaItem{i18n.Russian: {
			"movie-438631": {ID: 438631, Title: "Дюна", MediaType: "movie", PosterPath: "/d-ru.jpg"},
		}},
		titleErr: titleErr,
	}
	s := newMediaServer(t, details, nil, "")
	sendAs(t, s, http.MethodPost, "/api/titles/state?type=movie&id=438631&state=watched")
	return s
}

func getIn(t *testing.T, s *Server, lang, path string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Accept-Language", lang)
	req.Header.Set("Datastar-Request", "true")
	req.AddCookie(signedIn(t, s))
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return html.UnescapeString(rec.Body.String())
}

// Library lists and recommendation headings name titles in the interface
// language, whatever language they were saved in.
func TestSavedTitlesAreNamedInTheInterfaceLanguage(t *testing.T) {
	s := localizedServer(t, nil)
	if page := getIn(t, s, "ru", "/lists/watched"); !strings.Contains(page, "Дюна") || !strings.Contains(page, "/d-ru.jpg") {
		t.Error("the Russian Watched list does not name Dune in Russian")
	}
	if rows := getIn(t, s, "ru", "/api/recommendations"); !strings.Contains(rows, "Дюна") {
		t.Errorf("the Russian recommendation heading does not name Dune in Russian:\n%s", rows)
	}
	if page := getIn(t, s, "en", "/lists/watched"); !strings.Contains(page, "Dune") {
		t.Error("the English Watched list lost Dune's saved name")
	}
}

// When TMDB cannot name a title, its saved name stays.
func TestASavedNameStaysWhenTMDBCannotGiveAnother(t *testing.T) {
	s := localizedServer(t, errors.New("tmdb is down"))
	if page := getIn(t, s, "ru", "/lists/watched"); !strings.Contains(page, `href="/movie/438631"`) || !strings.Contains(page, "Dune") {
		t.Error("with TMDB down the Watched list lost Dune")
	}
}

// Taking a title out of a custom list keeps the others named in the
// interface language.
func TestACustomListStaysLocalizedAfterAChange(t *testing.T) {
	s := localizedServer(t, nil)
	cookie := signedIn(t, s)
	send := func(method, path, body string) string {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Accept-Language", "ru")
		req.Header.Set("Datastar-Request", "true")
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s = %d %s", method, path, rec.Code, rec.Body)
		}
		return html.UnescapeString(rec.Body.String())
	}
	send(http.MethodPost, "/api/lists?type=movie&id=438631", `{"listName":"Sci-fi"}`)
	id := regexp.MustCompile(`/lists/c/([^"]+)"`).FindStringSubmatch(send(http.MethodGet, "/api/lists/tabs", ""))
	if id == nil {
		t.Fatal("no list was made")
	}
	send(http.MethodPost, "/api/lists/"+id[1]+"/titles?type=movie&id=693134", "")
	patched := send(http.MethodDelete, "/api/lists/"+id[1]+"/titles?type=movie&id=693134&from=list", "")
	if !strings.Contains(patched, "Дюна") {
		t.Errorf("after taking a title out the list is not in Russian:\n%s", patched)
	}
}
