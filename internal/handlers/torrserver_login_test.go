package handlers_test

import (
	"crypto/subtle"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/auth"
	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/handlers"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

// guardedTorrServer is a TorrServer started with --httpauth: /echo is open,
// everything else needs nas / nas-secret.
func guardedTorrServer(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/echo" {
			_, _ = w.Write([]byte("MatriX.141"))
			return
		}
		user, pass, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(user+":"+pass), []byte("nas:nas-secret")) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Authorization Required"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/torrents" {
			_, _ = w.Write([]byte(`[{"hash":"` + duneHash + `","title":"Dune on the NAS","stat":5}]`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func saveTorrServer(t *testing.T, l *local, admin *http.Cookie, signals string) string {
	t.Helper()
	return l.action(t, "/api/settings/sources/torrserver", signals, admin).Body.String()
}

func TestATorrServerWithALoginIsCheckedWithItAndUsed(t *testing.T) {
	nas := guardedTorrServer(t)
	l := newLocal(t, withAdmin(t))
	admin := l.admin(t)

	if body := saveTorrServer(t, l, admin, `{"torrserverMode":"external","torrserverUrl":"`+nas.URL+`"}`); !strings.Contains(body, "asks for a username and password") {
		t.Errorf("no login: the answer does not ask for one:\n%s", body)
	}
	if body := saveTorrServer(t, l, admin, `{"torrserverMode":"external","torrserverUrl":"`+nas.URL+`","torrserverUser":"nas","torrserverPassword":"wrong"}`); !strings.Contains(body, "refused the username or password") {
		t.Errorf("wrong password: the answer does not say so:\n%s", body)
	}
	if got := l.store.State().TorrServer.URL; got == nas.URL {
		t.Fatal("a TorrServer that refused the login was saved")
	}

	body := saveTorrServer(t, l, admin, `{"torrserverMode":"external","torrserverUrl":"`+nas.URL+`","torrserverUser":"nas","torrserverPassword":"nas-secret"}`)
	if !strings.Contains(body, "Connected to TorrServer MatriX.141") || strings.Contains(body, "nas-secret") {
		t.Fatalf("right login: not connected, or the password came back:\n%s", body)
	}
	if page := l.do(t, httptest.NewRequest(http.MethodGet, "/torrserver", nil), admin).Body.String(); !strings.Contains(page, "Dune on the NAS") {
		t.Error("the TorrServer page does not list the NAS's torrents with the saved login")
	}
	if page := l.do(t, httptest.NewRequest(http.MethodGet, "/settings/sources", nil), admin).Body.String(); strings.Contains(page, "nas-secret") || !strings.Contains(page, "A password is saved") {
		t.Error("Sources shows the saved password, or does not say one is saved")
	}

	// Saving again without typing the password keeps it.
	if body := saveTorrServer(t, l, admin, `{"torrserverMode":"external","torrserverUrl":"`+nas.URL+`","torrserverUser":"nas","torrserverPassword":""}`); !strings.Contains(body, "Connected") {
		t.Errorf("re-saving with an empty password lost the saved one:\n%s", body)
	}
}

func TestALoginTypedIntoTheAddressIsTakenOutOfIt(t *testing.T) {
	nas := guardedTorrServer(t)
	l := newLocal(t, withAdmin(t))
	withLogin := strings.Replace(nas.URL, "http://", "http://nas:nas-secret@", 1)
	body := saveTorrServer(t, l, l.admin(t), `{"torrserverMode":"external","torrserverUrl":"`+withLogin+`"}`)
	st := l.store.State().TorrServer
	if !strings.Contains(body, "Connected") || st.URL != nas.URL || st.User != "nas" {
		t.Errorf("saved %+v, want the address without the login and user nas:\n%s", st, body)
	}
}

func TestASavedLoginIsUsedAfterARestart(t *testing.T) {
	nas := guardedTorrServer(t)
	l := newLocal(t, func(st *config.Store) {
		addAccount(t, st, "admin", config.RoleAdmin)
		_ = st.Update(func(s *config.State) error {
			s.TorrServer = config.TorrServer{Mode: config.EngineExternal, URL: nas.URL, User: "nas", Password: "nas-secret"}
			return nil
		})
	}, func(c *handlers.Config) { c.TorrServer = nil }) // the server builds its own, as at start-up
	page := l.do(t, httptest.NewRequest(http.MethodGet, "/torrserver", nil), l.signIn(t, &auth.User{Username: "admin", Role: config.RoleAdmin})).Body.String()
	if !strings.Contains(page, "Dune on the NAS") {
		t.Error("after a restart the saved login is not used")
	}
}

func TestParsingATorrServerAddressSeparatesItsLogin(t *testing.T) {
	addr, user, pass, err := torrserver.ParseServerURL(" http://nas:p%40ss@192.168.1.5:8090/ ")
	if err != nil || addr != "http://192.168.1.5:8090" || user != "nas" || pass != "p@ss" {
		t.Errorf("ParseServerURL = %q %q %q %v", addr, user, pass, err)
	}
}
