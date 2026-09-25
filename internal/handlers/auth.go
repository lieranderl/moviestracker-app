// Package handlers provides HTTP request handlers for the DataStar webapp.
package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/auth"

	"github.com/starfederation/datastar-go/datastar"
)

const maxRequestBodyBytes = 1 << 20

// homePath is where signed-in users land: the trending catalog.
const homePath = "/movies"

type requestError struct {
	status int
	err    error
}

func (e *requestError) Error() string {
	return e.err.Error()
}

func (e *requestError) Unwrap() error {
	return e.err
}

type loginPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func isDatastarRequest(r *http.Request) bool {
	return r.Header.Get("Datastar-Request") == "true" ||
		strings.Contains(r.Header.Get("Accept"), "text/event-stream")
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.loginLimiter.Allow(clientIP(r, s.trustedProxies), time.Now()) {
		w.Header().Set("Retry-After", s.loginRetryAfter)
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return
	}
	if !requireConsent(w, r) {
		return
	}

	isDatastar := isDatastarRequest(r)
	payload, err := decodeLogin(w, r)
	if err != nil {
		writeRequestError(w, err)
		return
	}

	user, authErr := s.accounts.Authenticate(payload.Username, payload.Password)
	if authErr != nil {
		if isDatastar {
			sse := datastar.NewSSE(w, r)
			if err := sse.MarshalAndPatchSignals(map[string]any{
				"submitting":   false,
				"errorMessage": authErr.Error(),
			}); err != nil {
				logSSEError(r, "patch login error signals", err)
			}
			return
		}

		http.Redirect(w, r, "/login?error=invalid", http.StatusSeeOther)
		return
	}

	// Success: Issue secure session token and cookie
	token, err := s.sessions.CreateSession(user)
	if err != nil {
		if !errors.Is(err, auth.ErrSessionCapacity) {
			slog.Error("create session failed", "error", err)
		}
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}
	auth.SetSessionCookie(w, token, s.secureCookies)

	if isDatastar {
		sse := datastar.NewSSE(w, r)
		if err := patchRedirect(sse, homePath); err != nil {
			logSSEError(r, "patch login redirect", err)
		}
		return
	}

	http.Redirect(w, r, homePath, http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(auth.SessionCookieName); err == nil {
		s.sessions.DeleteSession(cookie.Value)
	}
	auth.ClearSessionCookie(w, s.secureCookies)

	if isDatastarRequest(r) {
		sse := datastar.NewSSE(w, r)
		if err := patchRedirect(sse, "/"); err != nil {
			logSSEError(r, "patch logout redirect", err)
		}
		return
	}

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) userFromRequest(r *http.Request) *auth.User {
	cookie, err := r.Cookie(auth.SessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}
	return s.sessions.GetUser(cookie.Value)
}

func decodeLogin(w http.ResponseWriter, r *http.Request) (loginPayload, error) {
	limited := http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return loginPayload{}, &requestError{status: http.StatusRequestEntityTooLarge, err: errors.New("request body too large")}
		}
		return loginPayload{}, &requestError{status: http.StatusBadRequest, err: errors.New("read request body")}
	}

	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return loginPayload{}, &requestError{status: http.StatusBadRequest, err: errors.New("invalid content type")}
	}

	var payload loginPayload
	switch mediaType {
	case "application/json":
		decoder := json.NewDecoder(bytes.NewReader(body))
		if err := decoder.Decode(&payload); err != nil {
			return loginPayload{}, &requestError{status: http.StatusBadRequest, err: errors.New("invalid JSON body")}
		}
		if err := ensureJSONEOF(decoder); err != nil {
			return loginPayload{}, err
		}
	case "application/x-www-form-urlencoded":
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return loginPayload{}, &requestError{status: http.StatusBadRequest, err: errors.New("invalid form body")}
		}
		payload.Username = values.Get("username")
		payload.Password = values.Get("password")
	default:
		return loginPayload{}, &requestError{status: http.StatusUnsupportedMediaType, err: fmt.Errorf("unsupported content type %q", mediaType)}
	}

	if strings.TrimSpace(payload.Username) == "" || payload.Password == "" {
		return loginPayload{}, &requestError{status: http.StatusBadRequest, err: errors.New("username and password are required")}
	}
	return payload, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return &requestError{status: http.StatusBadRequest, err: errors.New("invalid trailing JSON data")}
	}
	return nil
}

func writeRequestError(w http.ResponseWriter, err error) {
	var reqErr *requestError
	if errors.As(err, &reqErr) {
		http.Error(w, http.StatusText(reqErr.status), reqErr.status)
		return
	}
	http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
}

func patchRedirect(sse *datastar.ServerSentEventGenerator, destination string) error {
	return sse.MarshalAndPatchSignals(map[string]string{"redirectUrl": destination})
}
