package auth

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// testUser is a signed-in local account.
var testUser = &User{Username: "alex", Name: "Alex Rivers", Role: "admin"}

type errorReader struct {
	err error
}

func (r errorReader) Read([]byte) (int, error) {
	return 0, r.err
}

type countingReader struct {
	next byte
}

func (r *countingReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.next
	}
	r.next++
	return len(p), nil
}

func TestSessionManager(t *testing.T) {
	sm := NewSessionManager(8, &countingReader{})
	defer sm.Close()

	user := testUser

	token, err := sm.CreateSession(user)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	resolved := sm.GetUser(token)
	if resolved == nil || resolved.Username != user.Username {
		t.Errorf("expected user %v, got %v", user, resolved)
	}

	sm.DeleteSession(token)
	if sm.GetUser(token) != nil {
		t.Error("expected session to be deleted")
	}
}

func TestSessionManagerClose(t *testing.T) {
	sm := NewSessionManager(8, &countingReader{})

	var wg sync.WaitGroup
	for range 32 {
		wg.Go(sm.Close)
	}
	wg.Wait()
}

func TestSessionManagerCreateSessionFailsClosed(t *testing.T) {
	sourceErr := errors.New("entropy unavailable")
	sm := NewSessionManager(2, errorReader{err: sourceErr})
	defer sm.Close()

	token, err := sm.CreateSession(testUser)
	if token != "" {
		t.Fatalf("token = %q, want empty", token)
	}
	if !errors.Is(err, sourceErr) {
		t.Fatalf("error = %v, want wrapped %v", err, sourceErr)
	}
}

func TestSessionManagerCapacity(t *testing.T) {
	sm := NewSessionManager(2, &countingReader{})
	defer sm.Close()
	user := testUser

	for i := range 2 {
		if _, err := sm.CreateSession(user); err != nil {
			t.Fatalf("create session %d: %v", i, err)
		}
	}
	if _, err := sm.CreateSession(user); !errors.Is(err, ErrSessionCapacity) {
		t.Fatalf("error = %v, want %v", err, ErrSessionCapacity)
	}
}

func TestSessionManagerPrunesBeforeCapacityCheck(t *testing.T) {
	now := time.Unix(1_000, 0)
	sm := NewSessionManager(1, &countingReader{}, WithClock(func() time.Time { return now }))
	defer sm.Close()
	user := testUser

	token, err := sm.CreateSession(user)
	if err != nil {
		t.Fatalf("create first session: %v", err)
	}

	// Advance time past expiry
	now = now.Add(time.Duration(CookieMaxAge)*time.Second + time.Second)

	// Since capacity is 1, creating a replacement session succeeds by pruning the expired session
	replacementToken, err := sm.CreateSession(user)
	if err != nil {
		t.Fatalf("create replacement session: %v", err)
	}
	if sm.GetUser(token) != nil {
		t.Fatal("expired session should not be returned")
	}
	if sm.GetUser(replacementToken) == nil {
		t.Fatal("replacement session should be returned")
	}
}

func TestSessionExpires(t *testing.T) {
	now := time.Unix(1_000, 0)
	sm := NewSessionManager(1, &countingReader{}, WithClock(func() time.Time { return now }))
	defer sm.Close()
	token, err := sm.CreateSession(testUser)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Duration(CookieMaxAge)*time.Second + time.Nanosecond)
	if user := sm.GetUser(token); user != nil {
		t.Fatalf("expired session returned user %+v", user)
	}
}

func TestCookieHelpers(t *testing.T) {
	for _, secure := range []bool{false, true} {
		t.Run(map[bool]string{false: "development", true: "secure"}[secure], func(t *testing.T) {
			token := "sample-token-12345"
			rec := httptest.NewRecorder()
			SetSessionCookie(rec, token, secure)

			cookies := rec.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("cookie count = %d, want 1", len(cookies))
			}
			cookie := cookies[0]
			if cookie.Name != SessionCookieName || cookie.Value != token {
				t.Errorf("cookie = %s=%s, want %s=%s", cookie.Name, cookie.Value, SessionCookieName, token)
			}
			if cookie.Secure != secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
				t.Errorf("cookie flags = Secure:%t HttpOnly:%t SameSite:%v", cookie.Secure, cookie.HttpOnly, cookie.SameSite)
			}

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.AddCookie(cookie)
			readCookie, err := req.Cookie(SessionCookieName)
			if err != nil || readCookie.Value != token {
				t.Errorf("read cookie: value=%q error=%v", readCookie.Value, err)
			}

			clearRec := httptest.NewRecorder()
			ClearSessionCookie(clearRec, secure)
			cleared := clearRec.Result().Cookies()[0]
			if cleared.MaxAge != -1 || cleared.Secure != secure || !cleared.HttpOnly || cleared.SameSite != http.SameSiteLaxMode {
				t.Errorf("cleared cookie = %+v", cleared)
			}
		})
	}
}

func BenchmarkSessionManager(b *testing.B) {
	sm := NewSessionManager(1024, io.LimitReader(zeroReader{}, 1<<62))
	b.Cleanup(sm.Close)
	user := testUser

	b.ReportAllocs()
	for b.Loop() {
		token, err := sm.CreateSession(user)
		if err != nil {
			b.Fatal(err)
		}
		_ = sm.GetUser(token)
		sm.DeleteSession(token)
	}
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}
