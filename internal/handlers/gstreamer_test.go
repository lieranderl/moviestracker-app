package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/gstinstall"
	"github.com/lieranderl/moviestracker-app/internal/handlers"
)

// withGStreamerDownload gives the server the macOS app's GStreamer installer,
// run by a script that downloads slowly until the test ends.
func withGStreamerDownload(t *testing.T) localOption {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "install-gstreamer.sh")
	body := `#!/bin/sh
case "$1" in
  --version) echo 1.28.7; exit 0 ;;
  --size) echo 153594157; exit 0 ;;
esac
mkdir -p "$1/.download"
head -c 1000000 /dev/zero > "$1/.download/gstreamer.pkg"
sleep 30
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil { // #nosec G306 -- an executable fixture
		t.Fatal(err)
	}
	inst := gstinstall.New(script, filepath.Join(dir, "gstreamer"), nil)
	t.Cleanup(inst.Close)
	return func(c *handlers.Config) { c.GStreamer = inst }
}

func TestTheAdminInstallsGStreamerFromTheSourcesPage(t *testing.T) {
	engineOpt, _ := withEngine(t)
	l := newLocal(t, withAdmin(t), engineOpt, withGStreamerDownload(t))
	admin := l.admin(t)
	l.action(t, "/api/settings/sources/torrserver", `{"torrserverMode":"managed"}`, admin)
	if l.store.State().TorrServer.Mode != config.EngineManaged {
		t.Fatal("the managed engine did not start")
	}

	page := l.do(t, httptest.NewRequest(http.MethodGet, "/settings/sources", nil), admin).Body.String()
	if !strings.Contains(page, "Install GStreamer (146 MB)") || !strings.Contains(page, `/api/gstreamer`) {
		t.Fatalf("Sources page does not offer GStreamer:\n%s", page)
	}

	addAccount(t, l.store, "sam", config.RoleViewer)
	member := l.signIn(t, l.accounts.Lookup("sam"))
	if rr := l.action(t, "/api/gstreamer/install", `{}`, member); rr.Code != http.StatusForbidden {
		t.Errorf("a viewer starting the install got %d, want 403", rr.Code)
	}

	rr := l.action(t, "/api/gstreamer/install", `{}`, admin)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Downloading GStreamer 1.28.7") {
		t.Fatalf("starting the install = %d:\n%s", rr.Code, rr.Body.String())
	}
}

func TestGStreamerIsNotOfferedForSomeoneElsesTorrServer(t *testing.T) {
	engineOpt, _ := withEngine(t)
	l := newLocal(t, withAdmin(t), engineOpt, withGStreamerDownload(t))
	admin := l.admin(t)
	if err := l.store.Update(func(st *config.State) error {
		st.TorrServer.Mode, st.TorrServer.URL = config.EngineExternal, "http://127.0.0.1:1"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	page := l.do(t, httptest.NewRequest(http.MethodGet, "/settings/sources", nil), admin).Body.String()
	if strings.Contains(page, "Install GStreamer") {
		t.Error("GStreamer is offered although Moviestracker does not run TorrServer")
	}
	if rr := l.action(t, "/api/gstreamer/install", `{}`, admin); rr.Code != http.StatusNotFound {
		t.Errorf("install without the managed engine = %d, want 404", rr.Code)
	}
}

func TestTheDashboardOffersGStreamerWhenTheAppsTorrServerLacksIt(t *testing.T) {
	engineOpt, _ := withEngine(t)
	l := newLocal(t, withAdmin(t), engineOpt, withGStreamerDownload(t))
	admin := l.admin(t)
	l.action(t, "/api/settings/sources/torrserver", `{"torrserverMode":"managed"}`, admin)
	if page := l.do(t, httptest.NewRequest(http.MethodGet, "/dashboard", nil), admin).Body.String(); !strings.Contains(page, `id="gst-setup"`) {
		t.Error("the dashboard has no place for the GStreamer card")
	}
	body := openStreams(t, l, admin, 300*time.Millisecond, "/api/dashboard?stream=true")[0]
	if !strings.Contains(body, "Install GStreamer (146 MB)") || !strings.Contains(body, "install?compact=1") {
		t.Errorf("dashboard stream does not offer GStreamer:\n%s", body)
	}
}

func TestClosingTheGStreamerMessageNeedsASignInAndLeavesAnInstallRunning(t *testing.T) {
	engineOpt, _ := withEngine(t)
	l := newLocal(t, withAdmin(t), engineOpt, withGStreamerDownload(t))
	admin := l.admin(t)
	l.action(t, "/api/settings/sources/torrserver", `{"torrserverMode":"managed"}`, admin)
	l.action(t, "/api/gstreamer/install", `{}`, admin)

	if rr := l.action(t, "/api/gstreamer/dismiss?compact=1", `{}`, nil); rr.Code != http.StatusUnauthorized {
		t.Errorf("closing it signed out = %d, want 401", rr.Code)
	}
	rr := l.action(t, "/api/gstreamer/dismiss?compact=1", `{}`, admin)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Downloading GStreamer 1.28.7") {
		t.Errorf("closing it during the download = %d, want the download still shown:\n%s", rr.Code, rr.Body.String())
	}
}
