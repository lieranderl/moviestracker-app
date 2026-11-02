package web_test

import (
	"html"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/releases"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/web"
)

// A feed turned off brings no alerts, even for releases it held meanwhile;
// turned on again, its later releases do.
func TestAUserTurnsAFeedsAlertsOff(t *testing.T) {
	g := newGoogle(t)
	cfg := withTMDB(t, g.config())
	clock := now
	cfg.Store = store.NewMemory(store.WithClock(func() time.Time { return clock }))
	feeds := &swappedFeeds{src: releases.NewMemory(map[releases.Feed][]releases.Release{
		releases.DolbyVision: {{ID: 438631, Title: "Dune", FoundAt: now}},
		releases.HDR10:       {{ID: 329865, Title: "Arrival", FoundAt: now}},
	})}
	cfg.Releases = feeds
	cfg.Now = func() time.Time { return clock }
	h := web.New(cfg)
	session := signIn(t, h)
	if body := send(t, h, http.MethodPost, "/api/alerts/feeds?feed=hdr10movies&on=0", session).Body.String(); !strings.Contains(body, "_feedsOn") {
		t.Fatalf("turning HDR10 off answered:\n%s", body)
	}
	send(t, h, http.MethodPost, "/api/favorites?type=movie&id=438631", session)
	send(t, h, http.MethodPost, "/api/favorites?type=movie&id=329865", session)
	if got := bellCount(t, h, session); got != "1" {
		t.Errorf("with HDR10 off the bell shows %q, want 1 for Dune in Dolby Vision", got)
	}
	page := html.UnescapeString(getWith(t, h, "/alerts", session).Body.String())
	for _, want := range []string{`data-bind="_feedsOn.hdr10movies"`, `data-bind="_feedsOn.dvmovies"`, "@post('/api/alerts/feeds?feed=hdr10movies&on=' + ($_feedsOn.hdr10movies ? 1 : 0))"} {
		if !strings.Contains(page, want) {
			t.Errorf("the alerts page lacks %q", want)
		}
	}
	send(t, h, http.MethodPost, "/api/alerts/feeds?feed=hdr10movies&on=1", session)
	if got := bellCount(t, h, session); got != "" {
		t.Errorf("turning HDR10 back on alerted for a release it held while off (bell %q)", got)
	}
	send(t, h, http.MethodDelete, "/api/favorites?type=movie&id=438631", session) // the library changes
	if got := bellCount(t, h, session); got != "" {
		t.Errorf("after a library change HDR10 alerted for a release it held while off (bell %q)", got)
	}
	clock = clock.Add(10 * time.Minute)
	feeds.set(releases.NewMemory(map[releases.Feed][]releases.Release{
		releases.DolbyVision: {{ID: 438631, Title: "Dune", FoundAt: now}},
		releases.HDR10:       {{ID: 329865, Title: "Arrival", FoundAt: clock}},
	}))
	if got := bellCount(t, h, session); got != "1" {
		t.Errorf("Arrival's later HDR10 release shows the bell %q, want 1", got)
	}
}
