package web

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/lieranderl/moviestracker-app/internal/handlers"
	"github.com/lieranderl/moviestracker-app/internal/i18n"
)

const (
	sessionCookie = "mt_session"
	sessionTTL    = 30 * 24 * time.Hour
	// storeTimeout bounds each call to the user store, so a slow Firestore
	// cannot hold a page (the server itself has no write timeout).
	storeTimeout = 5 * time.Second
)

// User is a visitor signed in with Google.
type User struct {
	ID    string `json:"sub"` // Google's stable account ID
	Email string `json:"email"`
	Name  string `json:"name"`
}

// session is what the session cookie carries, signed.
type session struct {
	User
	Expires int64 `json:"exp"`
}

// currentUser is the visitor's user when they are signed in: a session
// cookie this app signed that has not expired.
func (a *app) currentUser(r *http.Request) (User, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || len(a.cfg.SessionKey) < 32 {
		return User{}, false
	}
	var s session
	if err := a.signer.open(c.Value, &s); err != nil || s.ID == "" || a.now().Unix() >= s.Expires {
		return User{}, false
	}
	return s.User, true
}

// handleSignInCallback is where Google sends the visitor back: the code is
// traded for an ID token of a verified Google account, which signs them in.
func (a *app) handleSignInCallback(w http.ResponseWriter, r *http.Request) {
	if !a.signInReady() {
		http.Error(w, i18n.T(r.Context(), "Sign-in is not set up."), http.StatusServiceUnavailable)
		return
	}
	// The attempt is used once, whatever comes of it.
	http.SetCookie(w, a.cookie(signInCookie, "", signInPath, -1))
	if r.URL.Query().Has("error") { // the visitor cancelled at Google
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	c, err := r.Cookie(signInCookie)
	var at attempt
	if err != nil || a.signer.open(c.Value, &at) != nil || a.now().Unix() >= at.Expires ||
		subtle.ConstantTimeCompare([]byte(at.State), []byte(r.URL.Query().Get("state"))) != 1 {
		http.Error(w, i18n.T(r.Context(), "This sign-in has expired or was not started here. Please sign in again."), http.StatusBadRequest)
		return
	}
	tok, err := a.oauth().Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(at.Verifier))
	if err != nil {
		slog.Warn("google sign-in: code exchange failed", "error", err)
		http.Error(w, i18n.T(r.Context(), "Google did not confirm the sign-in. Please sign in again."), http.StatusBadGateway)
		return
	}
	raw, _ := tok.Extra("id_token").(string)
	user, err := a.userFromIDToken(raw)
	if err != nil {
		slog.Warn("google sign-in: ID token refused", "error", err)
		http.Error(w, i18n.T(r.Context(), "This Google account cannot sign in here."), http.StatusForbidden)
		return
	}
	sealed, err := a.signer.seal(session{User: user, Expires: a.now().Add(sessionTTL).Unix()})
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, a.cookie(sessionCookie, sealed, "/", sessionTTL))
	// The language they picked before, on any browser.
	ctx, cancel := context.WithTimeout(r.Context(), storeTimeout)
	defer cancel()
	if prefs, err := a.cfg.Store.Preferences(ctx, user.ID); err != nil {
		slog.Warn("reading a user's preferences failed", "error", err)
	} else if lang, ok := i18n.Parse(prefs.Language); ok {
		handlers.RememberLanguage(w, lang, a.secure())
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// idClaims are the ID token claims the web app reads.
type idClaims struct {
	Issuer        string          `json:"iss"`
	Audience      json.RawMessage `json:"aud"` // a string or a list of them
	Subject       string          `json:"sub"`
	Email         string          `json:"email"`
	EmailVerified any             `json:"email_verified"` // true, or "true" in older tokens
	Name          string          `json:"name"`
	Expires       int64           `json:"exp"`
}

// userFromIDToken is the user an ID token names. The token came straight
// from Google's token endpoint over TLS, which authenticates it (OpenID
// Connect Core 1.0, 3.1.3.7, rule 6), so its signature is not checked; its
// issuer, audience and expiry are, and the email must be verified.
func (a *app) userFromIDToken(raw string) (User, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return User{}, errors.New("not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return User{}, fmt.Errorf("decode claims: %w", err)
	}
	var c idClaims
	if err := json.Unmarshal(payload, &c); err != nil {
		return User{}, fmt.Errorf("read claims: %w", err)
	}
	switch {
	case c.Issuer != "https://accounts.google.com" && c.Issuer != "accounts.google.com":
		return User{}, fmt.Errorf("issuer %q is not Google", c.Issuer)
	case !audienceHas(c.Audience, a.cfg.Google.ClientID):
		return User{}, errors.New("token is for another client")
	case a.now().Unix() >= c.Expires:
		return User{}, errors.New("token expired")
	case c.Subject == "":
		return User{}, errors.New("no account ID")
	case c.Email == "" || (c.EmailVerified != true && c.EmailVerified != "true"):
		return User{}, errors.New("email not verified")
	}
	return User{ID: c.Subject, Email: c.Email, Name: c.Name}, nil
}

func audienceHas(aud json.RawMessage, clientID string) bool {
	var one string
	if json.Unmarshal(aud, &one) == nil {
		return one == clientID
	}
	var many []string
	if json.Unmarshal(aud, &many) == nil {
		for _, a := range many {
			if a == clientID {
				return true
			}
		}
	}
	return false
}
