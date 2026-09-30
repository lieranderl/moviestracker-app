package handlers

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
)

// langCookieName keeps the interface language a browser chose.
const langCookieName = "mt_lang"

// requestLang is the language a request's pages are in: the one its browser
// chose, else the one it prefers.
func requestLang(r *http.Request) i18n.Lang {
	if c, err := r.Cookie(langCookieName); err == nil {
		if lang, ok := i18n.Parse(c.Value); ok {
			return lang
		}
	}
	return i18n.Negotiate(r.Header.Get("Accept-Language"))
}

// Language puts the request's language in its context, for pages and TMDB.
// The cloud web app (internal/web) uses it too.
func Language(next http.Handler) http.Handler { return language(next) }

// language puts the request's language in its context, for pages and TMDB.
func language(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(i18n.WithLang(r.Context(), requestLang(r))))
	})
}

// handleLanguage remembers the language a visitor picked and takes them
// back to the page they picked it on.
func (s *Server) handleLanguage(w http.ResponseWriter, r *http.Request) {
	SetLanguage(s.secureCookies)(w, r)
}

// SetLanguage handles POST /api/language: it remembers the language a
// visitor picked (secure: the cookie only travels over HTTPS) and takes them
// back to the page they picked it on.
func SetLanguage(secure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { setLanguage(w, r, secure) }
}

func setLanguage(w http.ResponseWriter, r *http.Request, secure bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	lang, ok := i18n.Parse(r.PostFormValue("lang"))
	if !ok {
		http.Error(w, i18n.T(r.Context(), "There is no such language."), http.StatusBadRequest)
		return
	}
	cookie := &http.Cookie{
		Name:     langCookieName,
		Value:    string(lang),
		Path:     "/",
		MaxAge:   consentMaxAge,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
	cookie.Secure = secure
	http.SetCookie(w, cookie)
	http.Redirect(w, r, backTo(r.Referer()), http.StatusSeeOther) // #nosec G710 -- backTo keeps only a path on this server
}

// backTo is the path of a page the Referer names, or "/" when it names none
// of ours (the host is not checked: cross-origin posts never get here).
func backTo(referer string) string {
	u, err := url.Parse(referer)
	if err != nil || !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") || strings.HasPrefix(u.Path, "/\\") || strings.HasPrefix(u.Path, "/api/") {
		return "/"
	}
	back := &url.URL{Path: u.Path, RawQuery: u.RawQuery}
	return back.String()
}
