package web_test

import (
	"context"
	"errors"
	"html"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/releases"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/web"
)

// A muted movie brings no release alerts while the others still do; once
// unmuted, its later releases alert again.
func TestAUserMutesAMoviesReleaseAlerts(t *testing.T) {
	g := newGoogle(t)
	cfg := withTMDB(t, g.config())
	users := store.NewMemory()
	cfg.Store = users
	feeds := &swappedFeeds{src: releases.NewMemory(map[releases.Feed][]releases.Release{
		releases.DolbyVision: {{ID: 438631, Title: "Dune", FoundAt: now}},
		releases.HDR10:       {{ID: 329865, Title: "Arrival", FoundAt: now}},
	})}
	cfg.Releases = feeds
	clock := now
	cfg.Now = func() time.Time { return clock }
	h := web.New(cfg)
	session := signIn(t, h)
	if body := send(t, h, http.MethodPost, "/api/alerts/mute?id=438631&on=1", session).Body.String(); !strings.Contains(body, "_muted") {
		t.Fatalf("muting Dune answered:\n%s", body)
	}
	send(t, h, http.MethodPost, "/api/favorites?type=movie&id=438631", session)
	send(t, h, http.MethodPost, "/api/favorites?type=movie&id=329865", session)
	if got := bellCount(t, h, session); got != "1" {
		t.Errorf("with Dune muted the bell shows %q, want 1 for Arrival", got)
	}
	page := html.UnescapeString(getWith(t, h, "/alerts", session).Body.String())
	for _, want := range []string{
		"@post('/api/alerts/mute?id=329865&on=1')", // Arrival's alert row
		"@post('/api/alerts/mute?id=438631&on=0')", // Dune among the muted
		"Muted movies",
		`href="/calendar"`, // air dates are elsewhere
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the alerts page lacks %q", want)
		}
	}
	if strings.Contains(page, `href="/movie/438631#sources"`) {
		t.Error("the alerts page lists an alert for muted Dune")
	}
	send(t, h, http.MethodPost, "/api/alerts/mute?id=438631&on=0", session)
	if muted, _ := users.MutedAlerts(context.Background(), "1098765"); len(muted) != 0 {
		t.Errorf("after unmuting, muted = %+v", muted)
	}
	clock = clock.Add(10 * time.Minute)
	feeds.set(releases.NewMemory(map[releases.Feed][]releases.Release{
		releases.DolbyVision: {{ID: 438631, Title: "Dune", FoundAt: clock}},
		releases.HDR10:       {{ID: 329865, Title: "Arrival", FoundAt: now}},
	}))
	if got := bellCount(t, h, session); got != "1" {
		t.Errorf("Dune's later release after unmuting shows the bell %q, want 1", got)
	}
}

// unreadableMutePreferences models the external preferences read failing.
type unreadableMutePreferences struct{ *store.Memory }

func (unreadableMutePreferences) MutedAlerts(context.Context, string) ([]store.MutedTitle, error) {
	return nil, errors.New("preferences unavailable")
}
func TestUnknownAlertPreferencesAreExplainedAndCannotBeChanged(t *testing.T) {
	g := newGoogle(t)
	cfg := g.config()
	users := unreadableMutePreferences{store.NewMemory()}
	cfg.Store = users
	if err := users.AddUpdates(context.Background(), "1098765", []store.Update{{Feed: "hdr10movies", TMDBID: 438631, Title: "Dune", FoundAt: now}}); err != nil {
		t.Fatal(err)
	}
	h := web.New(cfg)
	page := getWith(t, h, "/alerts", signIn(t, h))
	body := html.UnescapeString(page.Body.String())
	if page.Code != http.StatusServiceUnavailable || !strings.Contains(body, "Your alert preferences could not be loaded.") || !strings.Contains(body, "Retry") {
		t.Errorf("unknown preference state = %d without a retryable error", page.Code)
	}
	if strings.Contains(body, "@post('/api/alerts/mute") {
		t.Error("unknown mute preferences were rendered as active Mute controls")
	}
}

type unreadableFeedPreferences struct{ *store.Memory }

func (unreadableFeedPreferences) AlertFeeds(context.Context, string) (map[string]store.AlertFeed, error) {
	return nil, errors.New("feed preferences unavailable")
}
func TestUnknownFeedPreferencesAreExplainedAndDisabled(t *testing.T) {
	g := newGoogle(t)
	cfg := g.config()
	cfg.Store = unreadableFeedPreferences{store.NewMemory()}
	h := web.New(cfg)
	res := getWith(t, h, "/alerts", signIn(t, h))
	body := html.UnescapeString(res.Body.String())
	if res.Code != http.StatusServiceUnavailable || !strings.Contains(body, "Your alert preferences could not be loaded.") || !strings.Contains(body, `<fieldset class="fieldset" disabled>`) {
		t.Errorf("unreadable feed preferences = %d without disabled, explained controls", res.Code)
	}
}
