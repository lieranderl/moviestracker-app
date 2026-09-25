package handlers

import (
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/auth"
	"github.com/lieranderl/moviestracker-app/internal/views"

	"github.com/a-h/templ"
	"github.com/starfederation/datastar-go/datastar"
)

// setupGate sends every request to /setup until the first account exists.
// Only setup itself, static assets and health probes are reachable before.
func (s *Server) setupGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.accounts.NeedsSetup() && !reachableBeforeSetup(r.URL.Path) {
			if isDatastarRequest(r) {
				if err := patchRedirect(datastar.NewSSE(w, r), "/setup"); err != nil {
					logSSEError(r, "patch setup redirect", err)
				}
				return
			}
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func reachableBeforeSetup(path string) bool {
	switch path {
	case "/setup", "/api/setup", "/healthz", "/readyz":
		return true
	}
	return strings.HasPrefix(path, "/static/")
}

// handleRoot sends visitors to their home page or the sign-in page.
func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if s.userFromRequest(r) != nil {
		http.Redirect(w, r, homePath, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleSetupPage(w http.ResponseWriter, r *http.Request) {
	if !s.accounts.NeedsSetup() {
		s.handleRoot(w, r)
		return
	}
	templ.Handler(views.Setup(!fromThisMachine(r))).ServeHTTP(w, r)
}

// fromThisMachine reports whether a browser on the machine Moviestracker
// runs on sent r: a loopback peer that is not a proxy forwarding someone else.
func fromThisMachine(r *http.Request) bool {
	peer, ok := remoteIP(r.RemoteAddr)
	if !ok || !peer.IsLoopback() {
		return false
	}
	for _, h := range []string{"X-Forwarded-For", "X-Real-Ip", "Forwarded"} {
		if r.Header.Get(h) != "" {
			return false
		}
	}
	return true
}

// normalizeSetupCode ignores case, spaces and dashes, as people type codes.
func normalizeSetupCode(code string) string {
	return strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(code))
}

// setupAllowed reports whether r may create the first admin: from this
// machine, or from another device with the setup code.
func (s *Server) setupAllowed(r *http.Request, code string) bool {
	if fromThisMachine(r) {
		return true
	}
	code = normalizeSetupCode(code)
	return s.setupCode != "" && subtle.ConstantTimeCompare([]byte(code), []byte(s.setupCode)) == 1
}

type setupSignals struct {
	Code     string `json:"setupCode"`
	Accepted bool   `json:"accepted"`
	Username string `json:"username"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

// handleSetup creates the administrator of a fresh install, signs it in and
// continues to the Sources step.
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if !s.accounts.NeedsSetup() {
		http.Error(w, "Setup is already complete", http.StatusForbidden)
		return
	}
	if !s.loginLimiter.Allow(clientIP(r, s.trustedProxies), time.Now()) {
		w.Header().Set("Retry-After", s.loginRetryAfter)
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var sig setupSignals
	if err := datastar.ReadSignals(r, &sig); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if !s.setupAllowed(r, sig.Code) {
		if s.setupCode == "" {
			patchSetupError(w, r, "Set up Moviestracker in a browser on the machine it runs on: another device needs a setup code, and this server has none.")
		} else {
			patchSetupError(w, r, "Enter the setup code Moviestracker printed when it started.")
		}
		return
	}
	if !sig.Accepted {
		patchSetupError(w, r, "Please read and accept the disclaimer to continue.")
		return
	}
	user, err := s.accounts.CreateFirstAdmin(sig.Username, sig.Name, sig.Password)
	switch {
	case errors.Is(err, auth.ErrSetupDone):
		http.Error(w, "Setup is already complete", http.StatusForbidden)
		return
	case errors.Is(err, auth.ErrInvalidAccount):
		patchSetupError(w, r, "The "+err.Error()+".")
		return
	case err != nil:
		slog.Error("create first admin failed", "error", err)
		patchSetupError(w, r, "The account could not be saved. Check that the data directory is writable.")
		return
	}
	token, err := s.sessions.CreateSession(user)
	if err != nil {
		slog.Error("create session after setup failed", "error", err)
		patchSetupError(w, r, "The account was created; sign in to continue.")
		return
	}
	auth.SetSessionCookie(w, token, s.secureCookies)
	s.setConsentCookie(w)
	if err := patchRedirect(datastar.NewSSE(w, r), "/settings/sources?welcome=1"); err != nil {
		logSSEError(r, "patch setup redirect", err)
	}
}

func patchSetupError(w http.ResponseWriter, r *http.Request, message string) {
	sse := datastar.NewSSE(w, r)
	if err := sse.MarshalAndPatchSignals(map[string]any{"setupError": message, "password": ""}); err != nil {
		logSSEError(r, "patch setup error", err)
	}
}
