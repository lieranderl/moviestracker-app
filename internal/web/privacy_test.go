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
	} {
		if !strings.Contains(body, want) {
			t.Errorf("privacy policy lacks %q", want)
		}
	}
}

// The policy names every kind of data the app keeps for a user.
func TestThePrivacyPolicyListsEverythingKeptAboutAUser(t *testing.T) {
	body := html.UnescapeString(get(t, web.New(web.Config{}), "/privacy").Body.String())
	for _, want := range []string{
		"favourites",
		"Watched and Want to watch",
		"where you stopped watching",
		"release alerts",
		"your lists",
		"anyone with its link",
		"ratings and private notes",
		"release filters",
		"interface language",
		"TorrServers you add",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("privacy policy does not mention %q", want)
		}
	}
}

// Deletion and data requests go to a private address, never a public
// issue tracker.
func TestDataRequestsGoToAPrivateAddress(t *testing.T) {
	body := html.UnescapeString(get(t, web.New(web.Config{}), "/privacy").Body.String())
	if !strings.Contains(body, `href="mailto:moviestracker.scoured892@passinbox.com"`) {
		t.Error("privacy policy lacks the private contact address")
	}
	if strings.Contains(body, "/issues") {
		t.Error("privacy policy still sends data requests to the public issue tracker")
	}
}

// The policy promises removal in the app only for what the app can remove:
// playback progress and release alerts are deleted on request.
func TestThePrivacyPolicyPromisesOnlyTheRemovalTheAppOffers(t *testing.T) {
	body := html.UnescapeString(get(t, web.New(web.Config{}), "/privacy").Body.String())
	if strings.Contains(body, "change or remove any of this in the app") {
		t.Error("the policy says everything can be removed in the app, though progress and alerts cannot")
	}
	if !strings.Contains(body, "Where you stopped watching and your release alerts cannot be removed in the app yet") {
		t.Error("the policy does not say how to have progress and alerts removed")
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
