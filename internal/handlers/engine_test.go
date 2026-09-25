package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/engine"
	"github.com/lieranderl/moviestracker-app/internal/engine/enginetest"
	"github.com/lieranderl/moviestracker-app/internal/handlers"
)

// The test binary doubles as a fake TorrServer for the managed engine.
func TestMain(m *testing.M) {
	enginetest.RunFakeIfRequested()
	os.Exit(m.Run())
}

// withEngine gives the server a managed engine run from the fake TorrServer.
func withEngine(t *testing.T) (localOption, *engine.Supervisor) {
	t.Helper()
	sup := engine.New(engine.Config{
		Binary:       os.Args[0],
		Dir:          t.TempDir(),
		Port:         freePort(t),
		Env:          []string{enginetest.FakeEnv + "=1"},
		ReadyTimeout: 10 * time.Second,
	})
	t.Cleanup(func() { _ = sup.Stop() })
	return func(c *handlers.Config) { c.Engine = sup }, sup
}

func TestTheAdminCanHandTorrServerToMoviestrackerAndRestartIt(t *testing.T) {
	opt, sup := withEngine(t)
	l := newLocal(t, withAdmin(t), opt)
	admin := l.admin(t)

	page := l.do(t, httptest.NewRequest(http.MethodGet, "/settings/sources", nil), admin).Body.String()
	if !strings.Contains(page, "Managed by Moviestracker") {
		t.Fatalf("Sources page does not offer the managed engine")
	}

	rr := l.action(t, "/api/settings/sources/torrserver", `{"torrserverMode":"managed"}`, admin)
	if got := l.store.State().TorrServer.Mode; got != config.EngineManaged {
		t.Fatalf("mode after switching = %q, want managed:\n%s", got, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "MatriX.fake") {
		t.Errorf("response does not show the running engine:\n%s", rr.Body.String())
	}
	_, _, password := sup.Endpoint()
	if strings.Contains(rr.Body.String(), password) {
		t.Error("the engine password is sent to the browser")
	}
	status := l.do(t, httptest.NewRequest(http.MethodGet, "/api/torrserver/status", nil), admin).Body.String()
	if !strings.Contains(status, "MatriX.fake") {
		t.Errorf("TorrServer status does not use the managed engine:\n%s", status)
	}

	before := sup.Status().PID
	rr = l.action(t, "/api/settings/engine/restart", `{}`, admin)
	if after := sup.Status(); after.State != engine.Running || after.PID == before {
		t.Errorf("after restart: %+v (was PID %d):\n%s", after, before, rr.Body.String())
	}

	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("MatriX.145"))
	}))
	defer external.Close()
	l.action(t, "/api/settings/sources/torrserver", `{"torrserverMode":"external","torrserverUrl":"`+external.URL+`"}`, admin)
	if st := l.store.State().TorrServer; st.Mode != config.EngineExternal || st.URL != external.URL {
		t.Errorf("TorrServer after switching back = %+v", st)
	}
	if sup.Status().State != engine.Stopped {
		t.Errorf("the managed engine still runs after switching to an existing TorrServer: %+v", sup.Status())
	}
}

func TestTheManagedEngineIsUnavailableWithoutTheTorrServerProgram(t *testing.T) {
	l := newLocal(t, withAdmin(t))
	admin := l.admin(t)
	rr := l.action(t, "/api/settings/sources/torrserver", `{"torrserverMode":"managed"}`, admin)
	if l.store.State().TorrServer.Mode == config.EngineManaged || !strings.Contains(rr.Body.String(), "make torrserver") {
		t.Errorf("managed mode without a TorrServer program:\n%s", rr.Body.String())
	}
	if rr := l.action(t, "/api/settings/engine/restart", `{}`, admin); rr.Code != http.StatusNotFound {
		t.Errorf("restart without an engine: %d, want 404", rr.Code)
	}
}

func TestOnlyAnAdminRestartsTheEngine(t *testing.T) {
	opt, _ := withEngine(t)
	l := newLocal(t, func(store *config.Store) {
		addAccount(t, store, "admin", config.RoleAdmin)
		addAccount(t, store, "kid", config.RoleViewer)
	}, opt)
	if rr := l.action(t, "/api/settings/engine/restart", `{}`, l.signIn(t, l.accounts.Lookup("kid"))); rr.Code != http.StatusForbidden {
		t.Errorf("viewer restarting the engine: %d, want 403", rr.Code)
	}
}
