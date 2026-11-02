package web_test

import (
	"html"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/web"
)

// A followed series' episode airing brings an alert, once; an episode that
// aired before the user followed the series does not.
func TestAnEpisodeAiringAlertsItsFollowers(t *testing.T) {
	g := newGoogle(t)
	cfg := withTMDB(t, g.config())
	clock := now.Add(-48 * time.Hour) // followed two days before S01E09 airs
	cfg.Store = store.NewMemory(store.WithClock(func() time.Time { return clock }))
	cfg.Now = func() time.Time { return clock }
	h := web.New(cfg)
	session := signIn(t, h)
	send(t, h, http.MethodPost, "/api/follow?id=95396", session)
	clock = now // the day it airs
	if got := bellCount(t, h, session); got != "1" {
		t.Fatalf("after S01E09 aired the bell shows %q, want 1", got)
	}
	page := html.UnescapeString(getWith(t, h, "/alerts", session).Body.String())
	for _, want := range []string{"Severance", "S01E09", `href="/tv/95396"`, "Episode aired"} {
		if !strings.Contains(page, want) {
			t.Errorf("the alerts page lacks %q", want)
		}
	}
	clock = clock.Add(2 * time.Hour)
	if got := bellCount(t, h, session); got != "" {
		t.Errorf("the same episode alerted again (bell %q)", got)
	}
}

func TestAnEpisodeThatAiredBeforeFollowingBringsNoAlert(t *testing.T) {
	g := newGoogle(t)
	cfg := withTMDB(t, g.config())
	clock := now.Add(24 * time.Hour) // followed the day after S01E09 aired
	cfg.Store = store.NewMemory(store.WithClock(func() time.Time { return clock }))
	cfg.Now = func() time.Time { return clock }
	h := web.New(cfg)
	session := signIn(t, h)
	send(t, h, http.MethodPost, "/api/follow?id=95396", session)
	if got := bellCount(t, h, session); got != "" {
		t.Errorf("an episode aired before following alerted (bell %q)", got)
	}
}
