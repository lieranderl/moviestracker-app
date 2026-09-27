package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestPagesSpeakTheBrowsersLanguageUntilOneIsChosen(t *testing.T) {
	l := newLocal(t, withAdmin(t))

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	english := l.do(t, req, nil).Body.String()
	if !strings.Contains(english, `<html lang="en"`) || !strings.Contains(english, "Before you sign in") {
		t.Error("the sign-in page is not in English by default")
	}

	req = httptest.NewRequest(http.MethodGet, "/login", nil)
	req.Header.Set("Accept-Language", "ru-RU,ru;q=0.9,en;q=0.8")
	russian := l.do(t, req, nil).Body.String()
	if !strings.Contains(russian, `<html lang="ru"`) || !strings.Contains(russian, "Перед входом") {
		t.Error("a Russian browser does not get the sign-in page in Russian")
	}
}

func TestAVisitorCanSwitchLanguageAndStayOnTheirPage(t *testing.T) {
	l := newLocal(t, withAdmin(t))

	form := url.Values{"lang": {"ru"}}
	req := httptest.NewRequest(http.MethodPost, "/api/language", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "http://example.com/login?review=1")
	rr := l.do(t, req, nil)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/login?review=1" {
		t.Fatalf("switch = %d → %q, want 303 back to /login?review=1", rr.Code, rr.Header().Get("Location"))
	}
	var chosen *http.Cookie
	for _, c := range rr.Result().Cookies() {
		if c.Name == "mt_lang" {
			chosen = c
		}
	}
	if chosen == nil || chosen.Value != "ru" {
		t.Fatalf("language cookie = %+v", chosen)
	}

	req = httptest.NewRequest(http.MethodGet, "/login", nil)
	req.Header.Set("Accept-Language", "en-US")
	page := l.do(t, req, chosen).Body.String()
	if !strings.Contains(page, `<html lang="ru"`) {
		t.Error("the chosen language does not outrank the browser's")
	}
}

func TestLanguageSwitchRefusesUnknownLanguagesAndForeignReturnAddresses(t *testing.T) {
	l := newLocal(t, withAdmin(t))
	post := func(lang, referer string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/language", strings.NewReader(url.Values{"lang": {lang}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if referer != "" {
			req.Header.Set("Referer", referer)
		}
		return l.do(t, req, nil)
	}

	if rr := post("klingon", ""); rr.Code != http.StatusBadRequest {
		t.Errorf("unknown language = %d, want 400", rr.Code)
	}
	for _, referer := range []string{"", "not a url", "http://example.com//evil.example/x", "http://example.com/api/language"} {
		rr := post("en", referer)
		if loc := rr.Header().Get("Location"); rr.Code != http.StatusSeeOther || loc != "/" {
			t.Errorf("Referer %q → %d %q, want 303 to /", referer, rr.Code, loc)
		}
	}
}

func TestTheLanguageCanBeChosenBeforeSetup(t *testing.T) {
	l := newLocal(t, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/language", strings.NewReader("lang=ru"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", "http://example.com/setup")
	rr := l.do(t, req, nil)
	if rr.Code != http.StatusSeeOther || rr.Header().Get("Location") != "/setup" {
		t.Errorf("switch before setup = %d → %q", rr.Code, rr.Header().Get("Location"))
	}
}
