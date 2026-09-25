// Package auth provides local accounts, sessions and session cookies.
package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/lieranderl/moviestracker-app/internal/config"
)

const (
	SessionCookieName = "datastar_session"
	CookieMaxAge      = 7 * 24 * 60 * 60 // 7 days in seconds

	defaultSessionCleanupInterval = 10 * time.Minute
)

// User is a signed-in local account.
type User struct {
	Username string `json:"username"`
	Name     string `json:"name"`
	Role     string `json:"role"`
}

// IsAdmin reports whether the user may change settings and accounts.
func (u *User) IsAdmin() bool {
	return u != nil && u.Role == config.RoleAdmin
}

// Initials are what the avatar shows: the first letters of up to two words.
func (u *User) Initials() string {
	var out []rune
	for _, word := range strings.Fields(u.Name) {
		out = append(out, unicode.ToUpper([]rune(word)[0]))
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 {
		return "?"
	}
	return string(out)
}

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrSessionCapacity    = errors.New("session capacity reached")
)

// Option configures a SessionManager.
type Option func(*SessionManager)

// WithClock overrides the time source used for session creation, lookup, and expiry.
func WithClock(clock func() time.Time) Option {
	return func(sm *SessionManager) {
		if clock != nil {
			sm.now = clock
		}
	}
}

// WithStore keeps sessions in store so they survive a restart, and resolves
// each session's user through lookup, so a removed account signs out.
func WithStore(store *config.Store, lookup func(username string) *User) Option {
	return func(sm *SessionManager) {
		sm.store = store
		sm.lookup = lookup
	}
}

// SessionManager provides thread-safe session management with automatic TTL
// eviction. Sessions are keyed by a hash of their token, so the persisted
// copy cannot be replayed.
type SessionManager struct {
	mu          sync.RWMutex
	sessions    map[string]*sessionData // by tokenHash
	maxSessions int
	tokenSource io.Reader
	now         func() time.Time
	store       *config.Store
	lookup      func(username string) *User
	stopChan    chan struct{}
	doneChan    chan struct{}
	closeOnce   sync.Once
}

type sessionData struct {
	User      *User
	ExpiresAt time.Time
}

// NewSessionManager creates a bounded, thread-safe session store with an evictor goroutine.
func NewSessionManager(maxSessions int, tokenSource io.Reader, opts ...Option) *SessionManager {
	if maxSessions <= 0 {
		panic("auth: max sessions must be positive")
	}
	if tokenSource == nil {
		panic("auth: token source must not be nil")
	}

	sm := &SessionManager{
		sessions:    make(map[string]*sessionData),
		maxSessions: maxSessions,
		tokenSource: tokenSource,
		now:         time.Now,
		stopChan:    make(chan struct{}),
		doneChan:    make(chan struct{}),
	}
	for _, opt := range opts {
		opt(sm)
	}
	sm.load()
	// Start background cleaner ticker for expired sessions.
	go func() {
		defer close(sm.doneChan)
		ticker := time.NewTicker(defaultSessionCleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				sm.PruneExpired()
			case <-sm.stopChan:
				return
			}
		}
	}()
	return sm
}

// load restores the persisted sessions whose account still exists.
func (sm *SessionManager) load() {
	if sm.store == nil {
		return
	}
	now := sm.now()
	for _, rec := range sm.store.State().Sessions {
		if len(sm.sessions) >= sm.maxSessions || now.After(rec.ExpiresAt) {
			continue
		}
		if user := sm.lookup(rec.Username); user != nil {
			sm.sessions[rec.TokenHash] = &sessionData{User: user, ExpiresAt: rec.ExpiresAt}
		}
	}
}

// saveLocked writes the sessions to the store; the caller holds sm.mu.
func (sm *SessionManager) saveLocked() error {
	if sm.store == nil {
		return nil
	}
	records := make([]config.Session, 0, len(sm.sessions))
	for hash, sess := range sm.sessions {
		records = append(records, config.Session{TokenHash: hash, Username: sess.User.Username, ExpiresAt: sess.ExpiresAt})
	}
	return sm.store.Update(func(st *config.State) error {
		st.Sessions = records
		return nil
	})
}

// Close gracefully stops the background cleanup goroutine to prevent leaks.
func (sm *SessionManager) Close() {
	sm.closeOnce.Do(func() {
		close(sm.stopChan)
	})
	<-sm.doneChan
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateSession generates a secure token and associates it with a user.
func (sm *SessionManager) CreateSession(user *User) (string, error) {
	b := make([]byte, 24)
	if _, err := io.ReadFull(sm.tokenSource, b); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	token := hex.EncodeToString(b)
	hash := hashToken(token)

	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := sm.now()
	sm.pruneExpiredLocked(now)
	if len(sm.sessions) >= sm.maxSessions {
		return "", ErrSessionCapacity
	}

	sm.sessions[hash] = &sessionData{
		User:      user,
		ExpiresAt: now.Add(time.Duration(CookieMaxAge) * time.Second),
	}
	if err := sm.saveLocked(); err != nil {
		delete(sm.sessions, hash)
		return "", fmt.Errorf("save session: %w", err)
	}
	return token, nil
}

// GetUser retrieves the active user for the given session token.
func (sm *SessionManager) GetUser(token string) *User {
	if token == "" {
		return nil
	}

	sm.mu.RLock()
	sess, exists := sm.sessions[hashToken(token)]
	sm.mu.RUnlock()
	if !exists || sm.now().After(sess.ExpiresAt) {
		return nil
	}
	if sm.lookup != nil {
		return sm.lookup(sess.User.Username)
	}
	return sess.User
}

// DeleteSession invalidates a session token.
func (sm *SessionManager) DeleteSession(token string) {
	if token == "" {
		return
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	hash := hashToken(token)
	if _, ok := sm.sessions[hash]; !ok {
		return
	}
	delete(sm.sessions, hash)
	if err := sm.saveLocked(); err != nil {
		slog.Warn("could not save sessions after sign-out", "error", err)
	}
}

// EndSessionsOf signs username out of every browser, as when their account
// is deleted, demoted or given a new password.
func (sm *SessionManager) EndSessionsOf(username string) {
	sm.endSessionsOf(username, "")
}

// EndOtherSessionsOf signs username out of every browser but the one holding
// keepToken, as when they choose a new password themselves.
func (sm *SessionManager) EndOtherSessionsOf(username, keepToken string) {
	sm.endSessionsOf(username, hashToken(keepToken))
}

func (sm *SessionManager) endSessionsOf(username, keepHash string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	ended := false
	for hash, sess := range sm.sessions {
		if hash != keepHash && sess.User != nil && sess.User.Username == username {
			delete(sm.sessions, hash)
			ended = true
		}
	}
	if ended {
		if err := sm.saveLocked(); err != nil {
			slog.Warn("could not save sessions after signing an account out", "error", err)
		}
	}
}

// PruneExpired purges expired sessions to ensure bounded memory usage.
func (sm *SessionManager) PruneExpired() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.pruneExpiredLocked(sm.now()) {
		if err := sm.saveLocked(); err != nil {
			slog.Warn("could not save sessions after pruning", "error", err)
		}
	}
}

// pruneExpiredLocked drops expired sessions and reports whether any were.
func (sm *SessionManager) pruneExpiredLocked(now time.Time) bool {
	pruned := false
	for hash, sess := range sm.sessions {
		if now.After(sess.ExpiresAt) {
			delete(sm.sessions, hash)
			pruned = true
		}
	}
	return pruned
}

// SetSessionCookie sets a secure, HTTP-only session cookie on the response.
func SetSessionCookie(w http.ResponseWriter, token string, secure bool) {
	cookie := &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   CookieMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   true,
	}
	cookie.Secure = secure
	http.SetCookie(w, cookie)
}

// ClearSessionCookie clears the session cookie from the client.
func ClearSessionCookie(w http.ResponseWriter, secure bool) {
	cookie := &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   true,
		Expires:  time.Unix(0, 0),
	}
	cookie.Secure = secure
	http.SetCookie(w, cookie)
}
