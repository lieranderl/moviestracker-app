package update_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/update"
)

// latestRelease is what GitHub answers for releases/latest of v0.4.0.
const latestRelease = `{
  "tag_name": "v0.4.0",
  "html_url": "https://github.com/lieranderl/moviestracker-app/releases/tag/v0.4.0",
  "draft": false,
  "prerelease": false,
  "assets": [
    {"name": "Moviestracker-v0.4.0.dmg", "browser_download_url": "https://github.com/lieranderl/moviestracker-app/releases/download/v0.4.0/Moviestracker-v0.4.0.dmg"},
    {"name": "Moviestracker-Setup-v0.4.0-x64.exe", "browser_download_url": "https://github.com/lieranderl/moviestracker-app/releases/download/v0.4.0/Moviestracker-Setup-v0.4.0-x64.exe"},
    {"name": "checksums.txt", "browser_download_url": "https://github.com/lieranderl/moviestracker-app/releases/download/v0.4.0/checksums.txt"}
  ]
}`

// fakeGitHub answers releases/latest with body, tagged "r1" as GitHub does
// with an ETag, and counts the requests. A request for "r1" gets 304 Not
// Modified, which GitHub does not count against its rate limit.
func fakeGitHub(t *testing.T, body string) (*httptest.Server, *requests) {
	t.Helper()
	calls := &requests{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/lieranderl/moviestracker-app/releases/latest" {
			http.NotFound(w, r)
			return
		}
		calls.all++
		w.Header().Set("ETag", `"r1"`)
		if r.Header.Get("If-None-Match") == `"r1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		calls.full++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, calls
}

// requests counts what fakeGitHub was asked: all requests, and those that
// got the full release rather than 304 Not Modified.
type requests struct{ all, full int }

func TestWindowsUserSeesNewerReleaseWithItsInstaller(t *testing.T) {
	gh, _ := fakeGitHub(t, latestRelease)
	c := update.New("v0.3.2", update.Windows, update.WithAPI(gh.URL))

	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	got, ok := c.Available()
	if !ok {
		t.Fatal("no update available, want v0.4.0")
	}
	want := update.Release{
		Version:  "v0.4.0",
		Notes:    "https://github.com/lieranderl/moviestracker-app/releases/tag/v0.4.0",
		Download: "https://github.com/lieranderl/moviestracker-app/releases/download/v0.4.0/Moviestracker-Setup-v0.4.0-x64.exe",
	}
	if got != want {
		t.Errorf("Available() = %+v, want %+v", got, want)
	}
}

func TestNoUpdateWhenRunningTheLatestOrANewerVersion(t *testing.T) {
	for _, current := range []string{"v0.4.0", "v0.4.1", "v0.10.0", "v1.0.0"} {
		t.Run(current, func(t *testing.T) {
			gh, _ := fakeGitHub(t, latestRelease)
			c := update.New(current, update.Mac, update.WithAPI(gh.URL))

			if err := c.Refresh(context.Background()); err != nil {
				t.Fatalf("Refresh: %v", err)
			}

			if got, ok := c.Available(); ok {
				t.Errorf("Available() = %+v, want none for %s", got, current)
			}
		})
	}
}

func TestDevelopmentBuildNeverAsksGitHub(t *testing.T) {
	gh, calls := fakeGitHub(t, latestRelease)
	c := update.New("dev", update.Mac, update.WithAPI(gh.URL))

	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if calls.all != 0 {
		t.Errorf("GitHub was asked %d times, want 0", calls.all)
	}
	if _, ok := c.Available(); ok {
		t.Error("an update is available to a development build")
	}
}

func TestUnchangedReleaseIsNotDownloadedAgainAndKeepsTheNotice(t *testing.T) {
	gh, calls := fakeGitHub(t, latestRelease)
	c := update.New("v0.3.2", update.Mac, update.WithAPI(gh.URL))

	for i := range 2 {
		if err := c.Refresh(context.Background()); err != nil {
			t.Fatalf("Refresh %d: %v", i+1, err)
		}
	}

	if calls.all != 2 || calls.full != 1 {
		t.Errorf("GitHub sent the release %d times in %d requests, want once in 2", calls.full, calls.all)
	}
	got, ok := c.Available()
	if !ok || got.Version != "v0.4.0" {
		t.Errorf("Available() = %+v, %v; want v0.4.0", got, ok)
	}
}

func TestReleasesAreCheckedAtMostOnceADay(t *testing.T) {
	gh, calls := fakeGitHub(t, latestRelease)
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	c := update.New("v0.3.2", update.Mac, update.WithAPI(gh.URL), update.WithClock(func() time.Time { return now }))
	ctx := context.Background()

	c.RefreshIfDue(ctx)
	now = now.Add(23 * time.Hour)
	c.RefreshIfDue(ctx)
	if calls.all != 1 {
		t.Fatalf("GitHub was asked %d times within a day, want 1", calls.all)
	}

	now = now.Add(time.Hour)
	c.RefreshIfDue(ctx)
	if calls.all != 2 {
		t.Errorf("GitHub was asked %d times after a day, want 2", calls.all)
	}
}

func TestFailedCheckIsTriedAgainBeforeADayPasses(t *testing.T) {
	fail := true
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.Error(w, "rate limited", http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(latestRelease))
	}))
	t.Cleanup(gh.Close)
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	c := update.New("v0.3.2", update.Mac, update.WithAPI(gh.URL), update.WithClock(func() time.Time { return now }))

	c.RefreshIfDue(context.Background())
	fail = false
	now = now.Add(time.Hour)
	c.RefreshIfDue(context.Background())

	if _, ok := c.Available(); !ok {
		t.Error("no update available an hour after a failed check, want v0.4.0")
	}
}

func TestTurnedOffChecksNeverAskGitHubAndHideTheNotice(t *testing.T) {
	gh, calls := fakeGitHub(t, latestRelease)
	on := true
	c := update.New("v0.3.2", update.Mac, update.WithAPI(gh.URL), update.WithEnabled(func() bool { return on }))
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	on = false
	c.RefreshIfDue(context.Background())
	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	if calls.all != 1 {
		t.Errorf("GitHub was asked %d times, want only the check made while on", calls.all)
	}
	if got, ok := c.Available(); ok {
		t.Errorf("Available() = %+v while checks are off, want none", got)
	}
}

func TestOnlyGitHubLinksAreOffered(t *testing.T) {
	gh, _ := fakeGitHub(t, `{
  "tag_name": "v0.4.0",
  "html_url": "javascript:alert(1)",
  "assets": [{"name": "Moviestracker-v0.4.0.dmg", "browser_download_url": "http://example.com/Moviestracker-v0.4.0.dmg"}]
}`)
	c := update.New("v0.3.2", update.Mac, update.WithAPI(gh.URL))

	if err := c.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}

	want := update.Release{Version: "v0.4.0", Notes: "https://github.com/lieranderl/moviestracker-app/releases/tag/v0.4.0"}
	if got, ok := c.Available(); !ok || got != want {
		t.Errorf("Available() = %+v, %v; want %+v", got, ok, want)
	}
}

func TestPrereleaseBuildIsToldAboutItsStableRelease(t *testing.T) {
	for current, want := range map[string]bool{
		"v0.4.0-rc.1": true,  // v0.4.0 is the release it previews
		"v0.3.9-rc.2": true,  // an older pre-release
		"v0.4.1-rc.1": false, // previews something newer than v0.4.0
	} {
		t.Run(current, func(t *testing.T) {
			gh, calls := fakeGitHub(t, latestRelease)
			c := update.New(current, update.Mac, update.WithAPI(gh.URL))

			if err := c.Refresh(context.Background()); err != nil {
				t.Fatalf("Refresh: %v", err)
			}

			if calls.all != 1 {
				t.Errorf("GitHub was asked %d times, want 1", calls.all)
			}
			if _, ok := c.Available(); ok != want {
				t.Errorf("update available = %v, want %v", ok, want)
			}
		})
	}
}
