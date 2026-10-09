package web_test

import (
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/web"
)

func TestAnyoneCanReadThePrivacyPolicy(t *testing.T) {
	res := get(t, web.New(web.Config{}), "/privacy")
	if res.Code != http.StatusOK {
		t.Fatalf("GET /privacy = %d, want 200 for a signed-out visitor", res.Code)
	}
	body := html.UnescapeString(res.Body.String())
	for _, want := range []string{
		"<h1", "Privacy policy",
		"Google account", "favourites", "TorrServer",
		"usernames and passwords stay in your browser",
		"mt_session", "mt_lang",
		"https://github.com/lieranderl/moviestracker-app/issues",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("privacy policy lacks %q", want)
		}
	}
}

func TestThePrivacyPolicyIsTranslatedIntoRussian(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/privacy", nil)
	req.Header.Set("Accept-Language", "ru")
	web.New(web.Config{}).ServeHTTP(rec, req)
	if body := rec.Body.String(); !strings.Contains(body, "Политика конфиденциальности") {
		t.Errorf("Russian privacy policy lacks its title %q", "Политика конфиденциальности")
	}
}

func TestEveryWebAppPageLinksToThePrivacyPolicy(t *testing.T) {
	for _, path := range []string{"/", "/privacy"} {
		if body := get(t, web.New(web.Config{}), path).Body.String(); !strings.Contains(body, `href="/privacy"`) {
			t.Errorf("GET %s lacks a link to the privacy policy", path)
		}
	}
}
