package web_test

import (
	"context"
	"encoding/json"
	"errors"
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

func TestAccountDeletionEndsCachedSessionsOnAnotherInstanceBeforeWrites(t *testing.T) {
	clock := now
	g := newGoogle(t)
	cfg := withTMDB(t, g.config())
	cfg.Now = func() time.Time { return clock }
	users := store.NewMemory(store.WithClock(func() time.Time { return clock }))
	cfg.Store = users
	first, second := web.New(cfg), web.New(cfg)
	session := signIn(t, first)
	if res := getWith(t, second, "/account", session); res.Code != http.StatusOK {
		t.Fatal("second instance did not cache the signed-in session")
	}
	clock = clock.Add(time.Second)
	postSignals(t, first, "/api/account/delete", `{"deleteConfirm":"delete"}`, session)
	if page := getWith(t, second, "/account", session); page.Code != http.StatusSeeOther {
		t.Errorf("another instance still reads the deleted account: %d", page.Code)
	}
	res := send(t, second, http.MethodPost, "/api/favorites?type=movie&id=438631", session)
	if res.Code != http.StatusUnauthorized {
		t.Errorf("write through another instance after deletion = %d, want 401", res.Code)
	}
	if favs, _ := users.Favorites(context.Background(), "1098765"); len(favs) != 0 {
		t.Error("a cached session recreated deleted favourites")
	}
}

// heldProgress holds one external store call after request authentication.
type heldProgress struct {
	*store.Memory
	entered, release chan struct{}
}

func (s heldProgress) SaveProgress(ctx context.Context, uid string, p store.Progress) error {
	close(s.entered)
	select {
	case <-s.release:
		return s.Memory.SaveProgress(ctx, uid, p)
	case <-ctx.Done():
		return ctx.Err()
	}
}
func TestAnInflightProgressWriteCannotRecreateADeletedAccount(t *testing.T) {
	g := newGoogle(t)
	cfg := g.config()
	cfg.Now = func() time.Time { return now }
	users := heldProgress{Memory: store.NewMemory(store.WithClock(func() time.Time { return now })), entered: make(chan struct{}), release: make(chan struct{})}
	cfg.Store = users
	first, second := web.New(cfg), web.New(cfg)
	session := signIn(t, first)
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		result <- postSignals(t, second, "/api/progress", `{"hash":"aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111","index":1,"position":60,"duration":3600,"torrent":{"hash":"aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111","title":"Dune"}}`, session)
	}()
	select {
	case <-users.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("progress request did not reach the store")
	}
	postSignals(t, first, "/api/account/delete", `{"deleteConfirm":"delete"}`, session)
	close(users.release)
	select {
	case res := <-result:
		if res.Code == http.StatusNoContent {
			t.Error("the in-flight write succeeded after account deletion")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("progress write did not end")
	}
	exported, err := users.ExportAccount(context.Background(), "1098765")
	if err != nil {
		t.Fatal(err)
	}
	if len(exported.Progress) != 0 {
		t.Error("the in-flight request recreated deleted progress")
	}
}

// partialDeletion is an external store interrupted after recording revocation.
type partialDeletion struct {
	*store.Memory
	pending bool
	failed  bool
}

func (s *partialDeletion) DeleteAccount(ctx context.Context, uid string) error {
	if !s.failed {
		s.failed = true
		s.pending = true
		return errors.New("interrupted deletion")
	}
	if err := s.Memory.DeleteAccount(context.Background(), uid); err != nil {
		return err
	}
	s.pending = false
	return nil
}
func (s *partialDeletion) AccountDeletedAt(ctx context.Context, uid string) (time.Time, error) {
	if s.pending {
		return now, nil
	}
	return s.Memory.AccountDeletedAt(ctx, uid)
}
func (s *partialDeletion) AccountDeletionPending(context.Context, string) (bool, error) {
	return s.pending, nil
}
func TestAnInterruptedDeletionCanBeRetriedWithoutReauthorizingACompletedDeletion(t *testing.T) {
	g := newGoogle(t)
	cfg := g.config()
	cfg.Now = func() time.Time { return now }
	users := &partialDeletion{Memory: store.NewMemory(store.WithClock(func() time.Time { return now }))}
	cfg.Store = users
	h := web.New(cfg)
	session := signIn(t, h)
	if err := users.AddFavorite(context.Background(), "1098765", store.Favorite{Kind: "movie", TMDBID: 438631, Title: "Dune"}); err != nil {
		t.Fatal(err)
	}
	failed := postSignals(t, h, "/api/account/delete", `{"deleteConfirm":"delete"}`, session)
	if !strings.Contains(failed.Body.String(), "could not be deleted completely") {
		t.Fatal("the external deletion failure was not reported")
	}
	retry := postSignals(t, h, "/api/account/delete", `{"deleteConfirm":"delete"}`, session)
	if retry.Code == http.StatusUnauthorized {
		t.Error("the still-pending deletion cannot be retried with its original session")
	}
	if favs, _ := users.Favorites(context.Background(), "1098765"); len(favs) != 0 {
		t.Error("retry left account data behind")
	}
	if users.pending {
		return
	}
	again := postSignals(t, h, "/api/account/delete", `{"deleteConfirm":"delete"}`, session)
	if again.Code != http.StatusUnauthorized {
		t.Error("a completed deletion reauthorized the old session")
	}
}

func TestSigningInWithinTheDeletionSecondStartsAFreshSession(t *testing.T) {
	clock := now
	g := newGoogle(t)
	cfg := g.config()
	cfg.Now = func() time.Time { return clock }
	cfg.Store = store.NewMemory(store.WithClock(func() time.Time { return clock }))
	h := web.New(cfg)
	session := signIn(t, h)
	clock = clock.Add(time.Millisecond)
	postSignals(t, h, "/api/account/delete", `{"deleteConfirm":"delete"}`, session)
	clock = clock.Add(time.Millisecond)
	again := signIn(t, h)
	if res := getWith(t, h, "/account", again); res.Code != http.StatusOK {
		t.Errorf("fresh sign-in within the deletion second = %d, want 200", res.Code)
	}
}

func TestSigningInAgainCompletesAPendingDeletionBeforeStartingFresh(t *testing.T) {
	g := newGoogle(t)
	cfg := g.config()
	users := &partialDeletion{Memory: store.NewMemory()}
	cfg.Store = users
	h := web.New(cfg)
	session := signIn(t, h)
	if err := users.AddFavorite(context.Background(), "1098765", store.Favorite{Kind: "movie", TMDBID: 438631, Title: "Dune"}); err != nil {
		t.Fatal(err)
	}
	postSignals(t, h, "/api/account/delete", `{"deleteConfirm":"delete"}`, session)
	again := signIn(t, h)
	if users.pending {
		t.Error("a new sign-in left the requested account deletion pending")
	}
	if favs, _ := users.Favorites(context.Background(), "1098765"); len(favs) != 0 {
		t.Error("the new account retained data from its interrupted deletion")
	}
	if res := getWith(t, h, "/account", again); res.Code != http.StatusOK {
		t.Errorf("the new session = %d, want 200", res.Code)
	}
}
