package web_test

import (
	"encoding/json"
	"html"
	"regexp"
	"slices"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/web"
)

func TestSavedTorrServersRemainSelectableWhenTheSelectorIsPatched(t *testing.T) {
	g := newGoogle(t)
	cfg := withTMDB(t, g.config())
	users := store.NewMemory()
	cfg.Store = users
	if _, err := users.SaveTorrServer(t.Context(), "1098765", store.TorrServer{Name: "TVbox", URL: "https://tvbox.example:8091"}); err != nil {
		t.Fatal(err)
	}
	h := web.New(cfg)
	session := signIn(t, h)
	res := getWith(t, h, "/api/ts/selector", session)
	if res.Code != 200 {
		t.Fatalf("selector = %d", res.Code)
	}
	match := regexp.MustCompile(`data-server-addresses="([^"]*)"`).FindStringSubmatch(res.Body.String())
	if len(match) != 2 {
		t.Fatalf("selector lacks saved server data: %s", res.Body.String())
	}
	var urls []string
	if err := json.Unmarshal([]byte(html.UnescapeString(match[1])), &urls); err != nil {
		t.Fatalf("saved server data is not JSON: %q (%v)", html.UnescapeString(match[1]), err)
	}
	if !slices.Equal(urls, []string{"https://tvbox.example:8091"}) {
		t.Errorf("saved server choices = %v", urls)
	}
}
