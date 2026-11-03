package handlers

import (
	"errors"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"html"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestAUserSavesAndAppliesTheirDiscoveryFilters(t *testing.T) {
	s := newMediaServer(t, discoverDetails(), nil, "")
	cookie := signedIn(t, s)
	post := httptest.NewRequest(http.MethodPost, "/api/discovery-presets?type=movie&genre=878&year=2021&rating=7&runtime=120&lang=fr&sort=rating&page=9&pick=3&unknown=bad", strings.NewReader(`{"presetName":"Friday"}`))
	post.Header.Set("Content-Type", "application/json")
	post.Header.Set("Datastar-Request", "true")
	post.AddCookie(cookie)
	saved := httptest.NewRecorder()
	s.ServeHTTP(saved, post)
	if saved.Code != http.StatusOK {
		t.Fatalf("save preset = %d; want 200", saved.Code)
	}
	body := html.UnescapeString(saved.Body.String())
	apply := regexp.MustCompile(`/discover/presets/[a-z0-9]+`).FindString(body)
	if apply == "" || !strings.Contains(body, "Friday") {
		t.Fatalf("saving did not show the named preset and Apply link: %s", body)
	}
	get := httptest.NewRequest(http.MethodGet, apply, nil)
	get.AddCookie(cookie)
	applied := httptest.NewRecorder()
	s.ServeHTTP(applied, get)
	const want = "/discover?genre=878&lang=fr&rating=7&runtime=120&sort=rating&type=movie&year=2021"
	if applied.Code != http.StatusSeeOther || applied.Header().Get("Location") != want {
		t.Errorf("apply = %d to %q; want 303 to %q", applied.Code, applied.Header().Get("Location"), want)
	}
}

func TestAUserRenamesAndDeletesTheirSavedDiscoveryFilters(t *testing.T) {
	s := newMediaServer(t, discoverDetails(), nil, "")
	cookie := signedIn(t, s)
	send := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Datastar-Request", "true")
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	made := send(http.MethodPost, "/api/discovery-presets?type=movie&runtime=120", `{"presetName":"Friday"}`)
	apply := regexp.MustCompile(`/discover/presets/([a-z0-9]+)`).FindStringSubmatch(html.UnescapeString(made.Body.String()))
	if len(apply) != 2 {
		t.Fatalf("no saved preset: %d", made.Code)
	}
	renamed := send(http.MethodPost, "/api/discovery-presets/"+apply[1]+"/rename?type=movie&runtime=90", `{"presetName":"Sunday"}`)
	if renamed.Code != http.StatusOK || !strings.Contains(renamed.Body.String(), "Sunday") {
		t.Fatalf("rename = %d; want updated named preset", renamed.Code)
	}
	applied := send(http.MethodGet, apply[0], "")
	if applied.Header().Get("Location") != "/discover?runtime=120&sort=popular&type=movie" {
		t.Errorf("rename changed filters: %s", applied.Header().Get("Location"))
	}
	for range 2 {
		removed := send(http.MethodDelete, "/api/discovery-presets/"+apply[1], "")
		if removed.Code != http.StatusOK || strings.Contains(removed.Body.String(), apply[0]) {
			t.Errorf("delete = %d; preset remains in response", removed.Code)
		}
	}
	if gone := send(http.MethodGet, apply[0], ""); gone.Code != http.StatusNotFound {
		t.Errorf("deleted preset = %d; want 404", gone.Code)
	}
}

func TestAFailedPresetSaveKeepsTheFormAndShowsAnError(t *testing.T) {
	cfg := validTestConfig(t)
	cfg.Details = discoverDetails()
	s := newTestServerWithConfig(t, cfg)
	cookie := signedIn(t, s)
	dir := cfg.Store.Dir()
	if err := os.Rename(dir, dir+"-unavailable"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Rename(dir+"-unavailable", dir) })
	r := httptest.NewRequest(http.MethodPost, "/api/discovery-presets?type=movie", strings.NewReader(`{"presetName":"Friday"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Datastar-Request", "true")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	body := html.UnescapeString(w.Body.String())
	if w.Code != http.StatusOK || !strings.Contains(body, `id="discovery-preset-status"`) || !strings.Contains(body, "could not be saved") {
		t.Errorf("failed save = %d; want visible retry feedback, got %s", w.Code, body)
	}
	if strings.Contains(body, `id="discovery-presets"`) {
		t.Error("failed save replaced the form and its unsaved input")
	}
}

func TestSavedFiltersRemainAvailableOnDiscoverReload(t *testing.T) {
	s := newMediaServer(t, discoverDetails(), nil, "")
	cookie := signedIn(t, s)
	r := httptest.NewRequest(http.MethodPost, "/api/discovery-presets?runtime=120", strings.NewReader(`{"presetName":"Friday"}`))
	r.Header.Set("Content-Type", "application/json")
	r.AddCookie(cookie)
	s.ServeHTTP(httptest.NewRecorder(), r)
	r = httptest.NewRequest(http.MethodGet, "/discover?runtime=90", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	body := html.UnescapeString(w.Body.String())
	for _, want := range []string{`id="discovery-presets"`, `Friday`, `aria-label="Preset name"`, `<dialog`, `Saved filters`, `/api/discovery-presets?runtime=90`} {
		if !strings.Contains(body, want) {
			t.Errorf("Discover reload lacks %q", want)
		}
	}
}

func TestApplyingObsoleteFiltersExplainsChangesWithoutChangingThePreset(t *testing.T) {
	cfg := validTestConfig(t)
	cfg.Details = discoverDetails()
	presets, err := store.OpenDiscoveryPresetFile(filepath.Join(cfg.Store.Dir(), "discovery-presets.json"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := presets.CreateDiscoveryPreset(t.Context(), testUsername, "Old filters", store.DiscoveryFilters{Kind: "movie", Genre: 9999, MaxRuntime: 100, Language: "xx", Sort: "popular"})
	if err != nil {
		t.Fatal(err)
	}
	s := newTestServerWithConfig(t, cfg)
	cookie := signedIn(t, s)
	r := httptest.NewRequest(http.MethodGet, "/discover/presets/"+p.ID, nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/discover?presetAdjusted=1&sort=popular&type=movie" {
		t.Fatalf("obsolete preset = %d to %q", w.Code, w.Header().Get("Location"))
	}
	r = httptest.NewRequest(http.MethodGet, w.Header().Get("Location"), nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), "your preset is unchanged") {
		t.Error("adjusted filters lack visible explanation")
	}
	reopened, err := store.OpenDiscoveryPresetFile(filepath.Join(cfg.Store.Dir(), "discovery-presets.json"))
	if err != nil {
		t.Fatal(err)
	}
	items, err := reopened.DiscoveryPresets(t.Context(), testUsername)
	if err != nil || len(items) != 1 || items[0].Filters != p.Filters {
		t.Errorf("apply mutated saved filters: %v, %v", items, err)
	}
}

func TestPresetLookupFailureOffersRetryWithoutApplyingBroaderFilters(t *testing.T) {
	for _, offline := range []string{"genres", "provider"} {
		t.Run(offline, func(t *testing.T) {
			cfg := validTestConfig(t)
			details := discoverDetails()
			details.genresErr = errors.New("offline")
			if offline == "genres" {
				cfg.Details = details
			}
			presets, err := store.OpenDiscoveryPresetFile(filepath.Join(cfg.Store.Dir(), "discovery-presets.json"))
			if err != nil {
				t.Fatal(err)
			}
			p, err := presets.CreateDiscoveryPreset(t.Context(), testUsername, "Science fiction", store.DiscoveryFilters{Kind: "movie", Genre: 878, Sort: "popular"})
			if err != nil {
				t.Fatal(err)
			}
			s := newTestServerWithConfig(t, cfg)
			r := httptest.NewRequest(http.MethodGet, "/discover/presets/"+p.ID, nil)
			r.AddCookie(signedIn(t, s))
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != http.StatusServiceUnavailable || w.Header().Get("Location") != "" || !strings.Contains(w.Body.String(), "Try again") {
				t.Errorf("offline apply = %d; want retry page without redirect", w.Code)
			}
		})
	}
}

func TestPresetMutationRejectsMalformedAndOversizedRequests(t *testing.T) {
	s := newMediaServer(t, discoverDetails(), nil, "")
	cookie := signedIn(t, s)
	for _, body := range []string{`{`, `{"presetName":"` + strings.Repeat("x", maxRequestBodyBytes) + `"}`} {
		r := httptest.NewRequest(http.MethodPost, "/api/discovery-presets", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("invalid body = %d; want 400", w.Code)
		}
	}
	r := httptest.NewRequest(http.MethodGet, "/discover", nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if strings.Contains(w.Body.String(), `href="/discover/presets/`) {
		t.Error("invalid requests created presets")
	}
}
