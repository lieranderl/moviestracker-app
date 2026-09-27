package handlers_test

import (
	"context"
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/handlers"
	"github.com/lieranderl/moviestracker-app/internal/update"
)

// releaseV040 is GitHub's latest release: v0.4.0, with the DMG and the
// Windows installer.
const releaseV040 = `{
  "tag_name": "v0.4.0",
  "html_url": "https://github.com/lieranderl/moviestracker-app/releases/tag/v0.4.0",
  "assets": [
    {"name": "Moviestracker-v0.4.0.dmg", "browser_download_url": "https://github.com/lieranderl/moviestracker-app/releases/download/v0.4.0/Moviestracker-v0.4.0.dmg"},
    {"name": "Moviestracker-Setup-v0.4.0-x64.exe", "browser_download_url": "https://github.com/lieranderl/moviestracker-app/releases/download/v0.4.0/Moviestracker-Setup-v0.4.0-x64.exe"}
  ]
}`

// withRelease makes the install run version on platform, with GitHub's
// latest release being body; the checker has already asked. Checks follow
// the administrator's setting, as in the apps.
func withRelease(t *testing.T, version string, platform update.Platform, body string) localOption {
	t.Helper()
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(gh.Close)
	return func(c *handlers.Config) {
		store := c.Store
		checker := update.New(version, platform, update.WithAPI(gh.URL),
			update.WithEnabled(func() bool { return !store.State().Updates.Off }))
		if err := checker.Refresh(context.Background()); err != nil {
			t.Fatalf("Refresh: %v", err)
		}
		c.Version = version
		c.Updates = checker
	}
}

func TestAdminsAreToldAboutANewReleaseOnEveryPage(t *testing.T) {
	l := newLocal(t, func(store *config.Store) {
		addAccount(t, store, "admin", config.RoleAdmin)
		addAccount(t, store, "kid", config.RoleViewer)
	}, withRelease(t, "v0.3.2", update.Mac, releaseV040))

	page := l.do(t, httpGet("/search"), l.admin(t)).Body.String()
	if !strings.Contains(page, "Moviestracker v0.4.0 is available") || !strings.Contains(page, `href="/settings/updates"`) {
		t.Errorf("an admin's page does not announce v0.4.0 with a link to Settings → Updates:\n%s", page)
	}

	page = l.do(t, httpGet("/search"), l.signIn(t, l.accounts.Lookup("kid"))).Body.String()
	if strings.Contains(page, "v0.4.0") {
		t.Error("a viewer, who cannot update, is told about v0.4.0")
	}
}

func TestUpdatesPageOffersThisPlatformsDownload(t *testing.T) {
	for _, tc := range []struct {
		platform update.Platform
		want     []string
	}{
		{update.Mac, []string{`href="https://github.com/lieranderl/moviestracker-app/releases/download/v0.4.0/Moviestracker-v0.4.0.dmg"`}},
		{update.Windows, []string{`href="https://github.com/lieranderl/moviestracker-app/releases/download/v0.4.0/Moviestracker-Setup-v0.4.0-x64.exe"`}},
		{update.Docker, []string{"docker compose pull", "ghcr.io/lieranderl/moviestracker:0.4.0"}},
	} {
		t.Run(string(tc.platform), func(t *testing.T) {
			l := newLocal(t, withAdmin(t), withRelease(t, "v0.3.2", tc.platform, releaseV040))

			rec := l.do(t, httpGet("/settings/updates"), l.admin(t))

			page := rec.Body.String()
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /settings/updates = %d", rec.Code)
			}
			for _, want := range append(tc.want, "v0.3.2", "Moviestracker v0.4.0 is available",
				`href="https://github.com/lieranderl/moviestracker-app/releases/tag/v0.4.0"`) {
				if !strings.Contains(page, want) {
					t.Errorf("the Updates page lacks %q", want)
				}
			}
		})
	}
}

func TestUpdatesPageSaysTheLatestVersionRuns(t *testing.T) {
	l := newLocal(t, withAdmin(t), withRelease(t, "v0.4.0", update.Mac, releaseV040))

	page := l.do(t, httpGet("/settings/updates"), l.admin(t)).Body.String()

	if !strings.Contains(page, "You have the latest version") {
		t.Errorf("the Updates page does not say v0.4.0 is the latest:\n%s", page)
	}
}

func TestOnlyAdminsSeeTheUpdatesPage(t *testing.T) {
	l := newLocal(t, func(store *config.Store) {
		addAccount(t, store, "admin", config.RoleAdmin)
		addAccount(t, store, "kid", config.RoleViewer)
	}, withRelease(t, "v0.3.2", update.Mac, releaseV040))

	rec := l.do(t, httpGet("/settings/updates"), l.signIn(t, l.accounts.Lookup("kid")))

	if rec.Code != http.StatusForbidden {
		t.Errorf("a viewer opening Updates: %d, want 403", rec.Code)
	}
}

func TestAdminCanTurnReleaseChecksOff(t *testing.T) {
	l := newLocal(t, withAdmin(t), withRelease(t, "v0.3.2", update.Mac, releaseV040))
	admin := l.admin(t)

	rec := l.action(t, "/api/settings/updates", `{"updatesCheck":false}`, admin)

	if rec.Code != http.StatusOK || !l.store.State().Updates.Off {
		t.Fatalf("turning checks off: %d, saved off = %v", rec.Code, l.store.State().Updates.Off)
	}
	if page := l.do(t, httpGet("/search"), admin).Body.String(); strings.Contains(page, "v0.4.0") {
		t.Error("pages still announce v0.4.0 with checks off")
	}
	if page := html.UnescapeString(l.do(t, httpGet("/settings/updates"), admin).Body.String()); !strings.Contains(page, `{"updatesCheck":false}`) {
		t.Errorf("the Updates page does not show checks as off:\n%s", page)
	}
}

func TestViewersCannotTurnReleaseChecksOff(t *testing.T) {
	l := newLocal(t, func(store *config.Store) {
		addAccount(t, store, "admin", config.RoleAdmin)
		addAccount(t, store, "kid", config.RoleViewer)
	}, withRelease(t, "v0.3.2", update.Mac, releaseV040))

	rec := l.action(t, "/api/settings/updates", `{"updatesCheck":false}`, l.signIn(t, l.accounts.Lookup("kid")))

	if rec.Code != http.StatusForbidden || l.store.State().Updates.Off {
		t.Errorf("a viewer turning checks off: %d, saved off = %v", rec.Code, l.store.State().Updates.Off)
	}
}

// onThisMachine is a request from the menu bar or tray app, on this machine.
func onThisMachine(path string) *http.Request {
	req := httpGet(path)
	req.RemoteAddr = "127.0.0.1:52100"
	return req
}

func TestTheMenuBarAndTrayAppsLearnAboutANewRelease(t *testing.T) {
	l := newLocal(t, withAdmin(t), withRelease(t, "v0.3.2", update.Windows, releaseV040))

	rec := l.do(t, onThisMachine("/api/update"), nil)

	want := `{"version":"v0.4.0","notes":"https://github.com/lieranderl/moviestracker-app/releases/tag/v0.4.0",` +
		`"download":"https://github.com/lieranderl/moviestracker-app/releases/download/v0.4.0/Moviestracker-Setup-v0.4.0-x64.exe"}`
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != want {
		t.Errorf("GET /api/update = %d %s, want 200 %s", rec.Code, rec.Body.String(), want)
	}
}

func TestTheAppsHearNothingWhenThereIsNoNewRelease(t *testing.T) {
	for name, opts := range map[string][]localOption{
		"latest runs": {withRelease(t, "v0.4.0", update.Mac, releaseV040)},
		"checks off": {withRelease(t, "v0.3.2", update.Mac, releaseV040), func(c *handlers.Config) {
			_ = c.Store.Update(func(st *config.State) error { st.Updates.Off = true; return nil })
		}},
		"development build": nil,
	} {
		t.Run(name, func(t *testing.T) {
			l := newLocal(t, withAdmin(t), opts...)

			if rec := l.do(t, onThisMachine("/api/update"), nil); rec.Code != http.StatusNoContent {
				t.Errorf("GET /api/update = %d %s, want 204", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestOtherDevicesCannotAskForReleases(t *testing.T) {
	l := newLocal(t, withAdmin(t), withRelease(t, "v0.3.2", update.Mac, releaseV040))
	req := httpGet("/api/update")
	req.RemoteAddr = "192.168.1.20:52100"

	if rec := l.do(t, req, nil); rec.Code != http.StatusForbidden {
		t.Errorf("GET /api/update from the network = %d, want 403", rec.Code)
	}
}
