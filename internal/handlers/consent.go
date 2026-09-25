package handlers

import (
	"errors"
	"net/http"

	"github.com/lieranderl/moviestracker-app/internal/views"

	"github.com/starfederation/datastar-go/datastar"
)

const (
	// consentCookieName records that this browser accepted the disclaimer.
	consentCookieName = "mt_consent"
	// consentVersion is bumped whenever the disclaimer text changes materially,
	// which re-prompts every browser.
	consentVersion = "v1"
	consentMaxAge  = 365 * 24 * 60 * 60
)

// hasConsent reports whether the request carries the current disclaimer acceptance.
func hasConsent(r *http.Request) bool {
	c, err := r.Cookie(consentCookieName)
	return err == nil && c.Value == consentVersion
}

// requireConsent redirects to the disclaimer step when it has not been accepted.
// It reports whether the caller may continue.
func requireConsent(w http.ResponseWriter, r *http.Request) bool {
	if hasConsent(r) {
		return true
	}
	if isDatastarRequest(r) {
		if err := patchRedirect(datastar.NewSSE(w, r), "/login"); err != nil {
			logSSEError(r, "patch consent redirect", err)
		}
		return false
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
	return false
}

type consentSignals struct {
	Accepted bool `json:"accepted"`
}

// handleConsent records explicit acceptance of the disclaimer. Datastar
// requests get the sign-in step patched in place; plain forms are redirected.
func (s *Server) handleConsent(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	accepted, err := readConsent(r)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			http.Error(w, "Request Entity Too Large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if !accepted {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	s.setConsentCookie(w)

	if !isDatastarRequest(r) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(views.SignInStep("")); err != nil {
		logSSEError(r, "patch sign-in step", err)
	}
}

func readConsent(r *http.Request) (bool, error) {
	if isDatastarRequest(r) {
		var signals consentSignals
		if err := datastar.ReadSignals(r, &signals); err != nil {
			return false, err
		}
		return signals.Accepted, nil
	}
	if err := r.ParseForm(); err != nil {
		return false, err
	}
	return r.PostForm.Get("accept") == "yes", nil
}

// setConsentCookie records that this browser accepted the disclaimer.
func (s *Server) setConsentCookie(w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:     consentCookieName,
		Value:    consentVersion,
		Path:     "/",
		MaxAge:   consentMaxAge,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
	cookie.Secure = s.secureCookies
	http.SetCookie(w, cookie)
}
