package handlers_test

import (
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/handlers"
	"github.com/lieranderl/moviestracker-app/internal/sources"
)

const projectKey = "jrp_project_key_fixture" // #nosec G101 -- a test fixture

// jacredRecorder answers every JacRed search empty and records who asked
// with which key.
type jacredRecorder struct {
	mu    sync.Mutex
	calls []string // "host key"
}

func (j *jacredRecorder) RoundTrip(r *http.Request) (*http.Response, error) {
	key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if key == "" {
		key = r.URL.Query().Get("apikey")
	}
	j.mu.Lock()
	j.calls = append(j.calls, r.URL.Host+" "+key)
	j.mu.Unlock()
	body := `{"results":[]}`
	if r.URL.Host != "api.jacred.su" {
		body = `[]`
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

func (j *jacredRecorder) last() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	if len(j.calls) == 0 {
		return ""
	}
	return j.calls[len(j.calls)-1]
}

// Releases carry Moviestracker's jacred.su project key: searches work with
// no key of one's own, the key is never shown, and it never goes to another
// JacRed.
func TestTheSharedJacRedKeyWorksOutOfTheBoxForJacredSuOnly(t *testing.T) {
	rec := &jacredRecorder{}
	l := newLocal(t, withAdmin(t), func(c *handlers.Config) {
		c.Connector = sources.Connector{JacRedHTTP: &http.Client{Transport: rec}}
		c.Env = config.Env{SharedJacRedKey: projectKey}
	})
	admin := l.admin(t)

	page := html.UnescapeString(l.do(t, httptest.NewRequest(http.MethodGet, "/settings/sources", nil), admin).Body.String())
	if strings.Contains(page, projectKey) {
		t.Fatal("the project key is sent to the browser")
	}
	if !strings.Contains(page, "Moviestracker's JacRed key is in use") || strings.Contains(page, "A key is saved") {
		t.Errorf("the JacRed card should say the shared key is in use, not that a key is saved")
	}

	// Test search and save with no key of one's own: the project key.
	l.action(t, "/api/settings/sources/jacred", `{"jacredUrl":"https://jacred.su","jacredApiKey":""}`, admin)
	if got := rec.last(); got != "api.jacred.su "+projectKey {
		t.Errorf("jacred.su was searched as %q, want with the project key", got)
	}
	if saved := l.store.State().Sources.JacRedAPIKey; saved != "" {
		t.Errorf("the project key was saved as one's own: %q", saved)
	}

	// Another JacRed never gets it.
	l.action(t, "/api/settings/sources/jacred", `{"jacredUrl":"http://nas:9117","jacredApiKey":""}`, admin)
	if got := rec.last(); got != "nas:9117 " {
		t.Errorf("a private JacRed was searched as %q, want without a key", got)
	}

	// Back to jacred.su with one's own key, then back to the shared one.
	l.action(t, "/api/settings/sources/jacred", `{"jacredUrl":"https://jacred.su","jacredApiKey":"mine"}`, admin)
	if got := rec.last(); got != "api.jacred.su mine" {
		t.Errorf("own key: searched as %q", got)
	}
	rr := l.action(t, "/api/settings/sources/jacred/shared", `{}`, admin)
	if saved := l.store.State().Sources.JacRedAPIKey; saved != "" || !strings.Contains(html.UnescapeString(rr.Body.String()), "Moviestracker's JacRed key is in use again") {
		t.Errorf("going back to the shared key left %q saved:\n%s", saved, rr.Body.String())
	}
}
