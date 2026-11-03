package web_test

import (
	"cloud.google.com/go/firestore"
	"context"
	"crypto/rand"
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/web"
)

func TestAnAccountOwnsItsSavedDiscoveryFilters(t *testing.T) {
	g := newGoogle(t)
	users := store.NewMemory()
	cfg := withTMDB(t, g.config())
	cfg.Store = users
	h := web.New(cfg)
	session := signIn(t, h)
	made := postSignals(t, h, "/api/discovery-presets?type=tv&runtime=90&lang=ru&pick=1&page=3", `{"presetName":"Короткий вечер"}`, session)
	if made.Code != http.StatusOK {
		t.Fatalf("save = %d: %s", made.Code, made.Body.String())
	}
	link := regexp.MustCompile(`/discover/presets/([a-z0-9]+)`).FindStringSubmatch(html.UnescapeString(made.Body.String()))
	if len(link) != 2 {
		t.Fatal("save lacks Apply link")
	}
	items, err := users.DiscoveryPresets(t.Context(), "1098765")
	if err != nil || len(items) != 1 || items[0].Name != "Короткий вечер" {
		t.Fatalf("preset is not owned by Google subject: %v, %v", items, err)
	}
	applied := getWith(t, h, link[0], session)
	if applied.Code != http.StatusSeeOther || applied.Header().Get("Location") != "/discover?lang=ru&runtime=90&sort=popular&type=tv" {
		t.Errorf("apply = %d to %s", applied.Code, applied.Header().Get("Location"))
	}
	page := getWith(t, h, "/discover", session)
	if !strings.Contains(page.Body.String(), "Короткий вечер") {
		t.Error("reload lost preset")
	}
	var exported store.Account
	if err := json.Unmarshal(getWith(t, h, "/account/export", session).Body.Bytes(), &exported); err != nil || len(exported.DiscoveryPresets) != 1 {
		t.Errorf("export omits preset: %v", err)
	}
	// A separate identity cannot read or modify this account's preset.
	other := newGoogle(t)
	other.claims["sub"] = "other-user"
	otherCfg := withTMDB(t, other.config())
	otherCfg.Store = users
	otherApp := web.New(otherCfg)
	otherSession := signIn(t, otherApp)
	if got := getWith(t, otherApp, link[0], otherSession); got.Code != http.StatusNotFound {
		t.Errorf("foreign apply = %d", got.Code)
	}
	rename := postSignals(t, otherApp, "/api/discovery-presets/"+link[1]+"/rename", `{"presetName":"Hijacked"}`, otherSession)
	if !strings.Contains(rename.Body.String(), "no longer exists") {
		t.Error("foreign rename lacks not-found feedback")
	}
	if got := postSignals(t, h, "/api/discovery-presets", `{"presetName":""}`, session); !strings.Contains(got.Body.String(), "1 to 60") {
		t.Error("invalid name lacks feedback")
	}
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		req := httptest.NewRequest(method, "/api/discovery-presets/"+link[1], strings.NewReader(`{}`))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code == http.StatusOK {
			t.Error("signed-out mutation accepted")
		}
	}
	if got := get(t, h, link[0]); got.Code == http.StatusSeeOther && strings.HasPrefix(got.Header().Get("Location"), "/discover?") {
		t.Error("signed-out apply accepted")
	}
}

func TestUnavailableSavedFiltersLeaveDiscoverUsableAndOfferRetry(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("requires Firestore emulator")
	}
	ctx := t.Context()
	users, err := store.NewFirestore(ctx, "demo-moviestracker", "moviestracker")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })
	client, err := firestore.NewClientWithDatabase(ctx, "demo-moviestracker", "moviestracker")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	uid := "preset-unavailable-" + rand.Text()
	doc := client.Collection("users").Doc(uid)
	t.Cleanup(func() { _, _ = doc.Delete(context.Background()) })
	g := newGoogle(t)
	g.claims["sub"] = uid
	cfg := withTMDB(t, g.config())
	cfg.Store = users
	h := web.New(cfg)
	session := signIn(t, h)
	if _, err := doc.Set(ctx, map[string]any{"discoveryPresets": "unreadable"}, firestore.MergeAll); err != nil {
		t.Fatal(err)
	}
	res := getWith(t, h, "/discover?runtime=90", session)
	body := html.UnescapeString(res.Body.String())
	for _, want := range []string{"Saved filters could not be loaded.", "Try again", `href=""`, "Nothing matches these filters"} {
		if !strings.Contains(body, want) {
			t.Errorf("failed-read page lacks %q", want)
		}
	}
	if _, err := doc.Update(ctx, []firestore.Update{{Path: "discoveryPresets", Value: firestore.Delete}}); err != nil {
		t.Fatal(err)
	}
	res = getWith(t, h, "/discover?runtime=90", session)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `id="discovery-preset-name"`) || strings.Contains(res.Body.String(), "Saved filters could not be loaded.") {
		t.Error("retry did not recover saved-filter controls")
	}
}
