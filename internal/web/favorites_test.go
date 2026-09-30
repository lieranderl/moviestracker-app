package web_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/web"
)

// favorites is a signed-in session in an app keeping favourites in users.
func favorites(t *testing.T) (h http.Handler, session *httptest.ResponseRecorder, users *store.Memory) {
	t.Helper()
	g := newGoogle(t)
	cfg := withTMDB(t, g.config())
	users = store.NewMemory()
	cfg.Store = users
	h = web.New(cfg)
	return h, signIn(t, h), users
}

// send is a Datastar request (as the favourite button sends) with the
// session's cookies.
func send(t *testing.T, h http.Handler, method, path string, session *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Datastar-Request", "true")
	for _, c := range session.Result().Cookies() {
		if c.MaxAge >= 0 {
			req.AddCookie(c)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAUserKeepsAMovieAsAFavourite(t *testing.T) {
	h, session, users := favorites(t)
	res := send(t, h, http.MethodPost, "/api/favorites?type=movie&id=438631", session)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `aria-pressed="true"`) {
		t.Fatalf("adding Dune = %d, want the button patched as pressed:\n%s", res.Code, res.Body)
	}
	favs, err := users.Favorites(context.Background(), "1098765")
	if err != nil || len(favs) != 1 || favs[0].Title != "Dune" || favs[0].Poster != "/d.jpg" || favs[0].Kind != "movie" {
		t.Errorf("Ann's favourites = %+v, %v, want Dune as TMDB names it", favs, err)
	}
	page := getWith(t, h, "/favorites", session)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `href="/movie/438631"`) {
		t.Errorf("GET /favorites = %d, want a page with Dune", page.Code)
	}
}

func TestAUserRemovesAFavourite(t *testing.T) {
	h, session, users := favorites(t)
	send(t, h, http.MethodPost, "/api/favorites?type=tv&id=95396", session)
	res := send(t, h, http.MethodDelete, "/api/favorites?type=tv&id=95396", session)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `aria-pressed="false"`) {
		t.Fatalf("removing Severance = %d, want the button patched as not pressed", res.Code)
	}
	if is, _ := users.IsFavorite(context.Background(), "1098765", "tv", 95396); is {
		t.Error("Severance is still a favourite after removing it")
	}
	if page := getWith(t, h, "/favorites", session).Body.String(); strings.Contains(page, `href="/tv/95396"`) {
		t.Error("the favourites page still shows Severance")
	}
}

func TestATitlePageShowsWhetherItIsAFavourite(t *testing.T) {
	h, session, _ := favorites(t)
	state := func() string {
		return send(t, h, http.MethodGet, "/api/favorites/state?type=movie&id=438631", session).Body.String()
	}
	if !strings.Contains(state(), `aria-pressed="false"`) {
		t.Error("before adding Dune, its button is pressed")
	}
	send(t, h, http.MethodPost, "/api/favorites?type=movie&id=438631", session)
	if !strings.Contains(state(), `aria-pressed="true"`) {
		t.Error("after adding Dune, its button is not pressed")
	}
	page := getWith(t, h, "/movie/438631", session).Body.String()
	if !strings.Contains(page, `/api/favorites/state?type=movie&amp;id=438631`) {
		t.Error("Dune's page does not load its favourite button")
	}
}

func TestOnlyRealTitlesCanBeFavourites(t *testing.T) {
	h, session, users := favorites(t)
	for _, path := range []string{"/api/favorites?type=movie&id=1", "/api/favorites?type=person&id=5", "/api/favorites?type=movie&id=x"} {
		if res := send(t, h, http.MethodPost, path, session); res.Code != http.StatusNotFound {
			t.Errorf("POST %s = %d, want 404", path, res.Code)
		}
	}
	if favs, _ := users.Favorites(context.Background(), "1098765"); len(favs) != 0 {
		t.Errorf("favourites = %+v, want none", favs)
	}
}

func TestFavouritesNeedASignedInUser(t *testing.T) {
	h, _, _ := favorites(t)
	nobody := httptest.NewRecorder()
	if res := send(t, h, http.MethodPost, "/api/favorites?type=movie&id=438631", nobody); res.Code != http.StatusUnauthorized {
		t.Errorf("adding signed out = %d, want 401", res.Code)
	}
	if res := get(t, h, "/favorites"); res.Code != http.StatusSeeOther || res.Header().Get("Location") != "/" {
		t.Errorf("GET /favorites signed out = %d to %q, want 303 to /", res.Code, res.Header().Get("Location"))
	}
}

// unreachableStore is a user store whose favourites cannot be read, as
// Firestore when it is down.
type unreachableStore struct{ *store.Memory }

func (unreachableStore) Favorites(context.Context, string) ([]store.Favorite, error) {
	return nil, errors.New("firestore: unavailable")
}

func TestFavouritesThatCannotBeReadAreNotShownAsNone(t *testing.T) {
	g := newGoogle(t)
	cfg := withTMDB(t, g.config())
	cfg.Store = unreachableStore{store.NewMemory()}
	h := web.New(cfg)
	page := getWith(t, h, "/favorites", signIn(t, h))
	body := page.Body.String()
	if page.Code != http.StatusServiceUnavailable || strings.Contains(body, "No favourites yet") || !strings.Contains(body, "could not be loaded") {
		t.Errorf("GET /favorites with the store down = %d, want 503 saying they could not be loaded, not that there are none", page.Code)
	}
}
