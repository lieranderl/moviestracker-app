package web_test

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/web"
)

// account is a signed-in user of an app whose store holds their Dune
// favourite.
func account(t *testing.T) (h http.Handler, session *httptest.ResponseRecorder, users *store.Memory) {
	t.Helper()
	g := newGoogle(t)
	cfg := withTMDB(t, g.config())
	users = store.NewMemory()
	cfg.Store = users
	h = web.New(cfg)
	session = signIn(t, h)
	if err := users.AddFavorite(context.Background(), "1098765", store.Favorite{Kind: "movie", TMDBID: 438631, Title: "Dune"}); err != nil {
		t.Fatal(err)
	}
	return h, session, users
}

// postSignals posts Datastar signals to path in the session.
func postSignals(t *testing.T, h http.Handler, path, signals string, session *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(signals))
	req.Header.Set("Datastar-Request", "true")
	req.Header.Set("Content-Type", "application/json")
	for _, c := range session.Result().Cookies() {
		if c.MaxAge >= 0 {
			req.AddCookie(c)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAUserDownloadsEverythingKeptAboutThem(t *testing.T) {
	h, session, _ := account(t)
	res := getWith(t, h, "/account/export", session)
	if res.Code != http.StatusOK || !strings.HasPrefix(res.Header().Get("Content-Disposition"), "attachment;") || !strings.Contains(res.Header().Get("Content-Type"), "application/json") {
		t.Fatalf("export = %d %q %q", res.Code, res.Header().Get("Content-Type"), res.Header().Get("Content-Disposition"))
	}
	var got store.Account
	if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil || len(got.Favorites) != 1 || got.Favorites[0].Title != "Dune" {
		t.Errorf("export = %s (%v), want the Dune favourite", res.Body, err)
	}
	if res := get(t, h, "/account/export"); res.Code == http.StatusOK {
		t.Error("a signed-out visitor downloaded an account")
	}
}

func TestTheYourDataPageOffersTheDownloadAndAConfirmedDeletion(t *testing.T) {
	h, session, _ := account(t)
	page := html.UnescapeString(getWith(t, h, "/account", session).Body.String())
	for _, want := range []string{`href="/account/export" download`, `<dialog id="account-delete"`, "data-bind:delete-confirm", "@post('/api/account/delete')", `href="/account"`} {
		if !strings.Contains(page, want) {
			t.Errorf("Your data page lacks %q", want)
		}
	}
}

// Deleting an account needs its confirmation; it then deletes everything
// and signs the user out.
func TestAUserDeletesTheirAccountAfterConfirmingIt(t *testing.T) {
	h, session, users := account(t)
	ctx := context.Background()
	refused := postSignals(t, h, "/api/account/delete", `{"deleteConfirm":"maybe"}`, session)
	if favs, _ := users.Favorites(ctx, "1098765"); len(favs) != 1 || !strings.Contains(refused.Body.String(), "account-delete-problem") {
		t.Fatalf("an unconfirmed deletion went ahead or said nothing:\n%s", refused.Body)
	}
	res := postSignals(t, h, "/api/account/delete", `{"deleteConfirm":" Delete "}`, session)
	if favs, _ := users.Favorites(ctx, "1098765"); len(favs) != 0 {
		t.Error("the confirmed deletion left the favourite")
	}
	signedOut := false
	for _, c := range res.Result().Cookies() {
		if c.Name == "mt_session" && c.MaxAge < 0 {
			signedOut = true
		}
	}
	if !signedOut {
		t.Error("deleting the account did not sign the user out")
	}
}

// Deleting an account signs it out everywhere: a session from another
// browser stops working, and signing in again starts afresh.
func TestDeletingAnAccountEndsItsOtherSessions(t *testing.T) {
	clock := now
	g := newGoogle(t)
	cfg := withTMDB(t, g.config())
	cfg.Now = func() time.Time { return clock }
	users := store.NewMemory(store.WithClock(func() time.Time { return clock }))
	cfg.Store = users
	h := web.New(cfg)
	session, other := signIn(t, h), signIn(t, h)
	clock = clock.Add(time.Second)
	postSignals(t, h, "/api/account/delete", `{"deleteConfirm":"delete"}`, session)
	clock = clock.Add(time.Second)
	if res := send(t, h, http.MethodGet, "/api/alerts/count", other); res.Code != http.StatusUnauthorized {
		t.Errorf("another browser's session still works after deletion: %d", res.Code)
	}
	if res := send(t, h, http.MethodPost, "/api/favorites?type=movie&id=438631", other); res.Code == http.StatusOK {
		t.Error("another browser's session recreated a favourite after deletion")
	}
	if favs, _ := users.Favorites(context.Background(), "1098765"); len(favs) != 0 {
		t.Errorf("the deleted account has favourites again: %+v", favs)
	}
	again := signIn(t, h)
	if res := send(t, h, http.MethodGet, "/api/alerts/count", again); res.Code != http.StatusOK {
		t.Errorf("signing in again after deletion = %d", res.Code)
	}
}
