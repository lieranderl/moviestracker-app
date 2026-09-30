package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
)

const (
	signInCookie = "mt_signin"    // one sign-in attempt: its state and PKCE verifier
	signInPath   = "/auth/google" // the attempt cookie travels only here and to the callback
	signInTTL    = 10 * time.Minute
)

// Google is the web app's Google OAuth client. AuthURL and TokenURL default
// to Google's.
type Google struct {
	ClientID     string
	ClientSecret string
	AuthURL      string
	TokenURL     string
}

// signer signs cookie values with HMAC-SHA256, so visitors cannot forge or
// change them.
type signer struct{ key []byte }

// seal is value as a cookie carries it: its JSON and the JSON's signature.
func (s signer) seal(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	return enc.EncodeToString(payload) + "." + enc.EncodeToString(s.mac(payload)), nil
}

// open reads a cookie value sealed by seal into value.
func (s signer) open(sealed string, value any) error {
	enc := base64.RawURLEncoding
	p, m, ok := strings.Cut(sealed, ".")
	if !ok {
		return errors.New("not a sealed value")
	}
	payload, err := enc.DecodeString(p)
	if err != nil {
		return err
	}
	mac, err := enc.DecodeString(m)
	if err != nil {
		return err
	}
	if !hmac.Equal(mac, s.mac(payload)) {
		return errors.New("signature does not match")
	}
	return json.Unmarshal(payload, value)
}

func (s signer) mac(payload []byte) []byte {
	h := hmac.New(sha256.New, s.key)
	h.Write(payload)
	return h.Sum(nil)
}

// attempt is a sign-in in progress, kept in the visitor's browser between
// the redirect to Google and the way back.
type attempt struct {
	State    string `json:"state"`
	Verifier string `json:"verifier"`
	Expires  int64  `json:"exp"`
}

// oauth is the OAuth client configuration of the web app's Google client.
func (a *app) oauth() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     a.cfg.Google.ClientID,
		ClientSecret: a.cfg.Google.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:   a.cfg.Google.AuthURL,
			TokenURL:  a.cfg.Google.TokenURL,
			AuthStyle: oauth2.AuthStyleInParams,
		},
		RedirectURL: a.cfg.BaseURL + signInPath + "/callback",
		Scopes:      []string{"openid", "email", "profile"},
	}
}

// handleSignIn sends the visitor to Google, remembering the attempt (a
// random state against forged callbacks, and the PKCE verifier) in a
// short-lived signed cookie.
func (a *app) handleSignIn(w http.ResponseWriter, r *http.Request) {
	if !a.signInReady() {
		http.Error(w, i18n.T(r.Context(), "Sign-in is not set up."), http.StatusServiceUnavailable)
		return
	}
	state := rand.Text()
	verifier := oauth2.GenerateVerifier()
	sealed, err := a.signer.seal(attempt{State: state, Verifier: verifier, Expires: a.now().Add(signInTTL).Unix()})
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, a.cookie(signInCookie, sealed, signInPath, signInTTL))
	target := a.oauth().AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oauth2.SetAuthURLParam("prompt", "select_account"))
	http.Redirect(w, r, target, http.StatusFound)
}

// cookie is an HttpOnly, SameSite=Lax cookie, Secure when the app is served
// over HTTPS; a negative maxAge deletes it.
func (a *app) cookie(name, value, path string, maxAge time.Duration) *http.Cookie {
	age := int(maxAge / time.Second)
	if maxAge < 0 {
		age = -1
	}
	c := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     path,
		MaxAge:   age,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
	// Plain HTTP only for local runs (http://localhost:8080).
	c.Secure = strings.HasPrefix(a.cfg.BaseURL, "https://")
	return c
}
