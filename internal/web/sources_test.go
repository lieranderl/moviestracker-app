package web_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cloud.google.com/go/firestore/apiv1/firestorepb"
	"github.com/lieranderl/moviestracker-app/internal/jacred"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/web"
	"google.golang.org/grpc"
)

func TestASignedInUserFindsMovieReleasesWithTheServersJacRedKey(t *testing.T) {
	var query, authorization string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query, authorization = r.URL.Query().Get("query"), r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"results":[{"title":"Dune 2021 2160p HDR","tracker":"rutracker","year":2021,"quality":2160,"video_type":"hdr","seeders":42,"magnet":"magnet:?xt=urn:btih:08ada5a7a6183aae1e09d831df6748d566095a10","source_url":"https://example.com/release"}]}`))
	}))
	t.Cleanup(fake.Close)
	g := newGoogle(t)
	cfg := withTMDB(t, g.config())
	cfg.Sources.Torrents = jacred.NewClient(fake.URL, jacred.WithSearchAPI(), jacred.WithAPIKey("jrs-server-key"))
	h := web.New(cfg)
	res := getWith(t, h, "/api/torrents?type=movie&id=438631&quality=2160&hdr=1", signIn(t, h))
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "Dune 2021 2160p HDR") {
		t.Fatalf("movie releases = %d %s, want Dune's HDR release", res.Code, res.Body)
	}
	if query != "Dune" || authorization != "Bearer jrs-server-key" {
		t.Errorf("JacRed search = %q with %q, want Dune with the server key", query, authorization)
	}
	if strings.Contains(res.Body.String(), "jrs-server-key") {
		t.Error("the server's JacRed key leaked into the response")
	}
}

func TestTitlePagesOfferBrowserSideSourcesAndTheUsersTorrServers(t *testing.T) {
	g := newGoogle(t)
	cfg := withTMDB(t, g.config())
	users := store.NewMemory()
	cfg.Store = users
	if _, err := users.SaveTorrServer(context.Background(), "1098765", store.TorrServer{Name: "Home", URL: "http://localhost:8090"}); err != nil {
		t.Fatal(err)
	}
	h := web.New(cfg)
	session := signIn(t, h)
	for _, path := range []string{"/movie/438631", "/tv/95396"} {
		page := getWith(t, h, path, session)
		for _, want := range []string{`id="sources"`, `/static/torrserver.js`, `/api/ts/selector`, `tsAddTorrent`, `data-media-id=`, `data-media-type=`, `data-title=`, `data-poster=`, `tsSelectorReady: false`, `$tsSelectorReady &amp;&amp; $tsSelected`} {
			if !strings.Contains(page.Body.String(), want) {
				t.Errorf("%s lacks %q", path, want)
			}
		}
		if strings.Contains(page.Body.String(), `/api/torrserver/status`) || strings.Contains(page.Body.String(), `/api/torrents/add`) {
			t.Errorf("%s contains local-app TorrServer actions", path)
		}
	}
	selector := getWith(t, h, "/api/ts/selector", session)
	if selector.Code != http.StatusOK || !strings.Contains(selector.Body.String(), "Home") || !strings.Contains(selector.Body.String(), "http://localhost:8090") || !strings.Contains(selector.Body.String(), "$tsSelectorReady = true") {
		t.Fatalf("selector = %d %s, want the user's Home server", selector.Code, selector.Body)
	}
}

// Datastar scans the page as soon as it runs; helpers that its expressions
// call must already exist, or one ReferenceError leaves the page inert.
func TestPagesLoadTheirBrowserHelpersBeforeDatastar(t *testing.T) {
	g := newGoogle(t)
	h := web.New(withTMDB(t, g.config()))
	session := signIn(t, h)
	for _, path := range []string{"/movie/438631", "/tv/95396", "/torrserver"} {
		body := getWith(t, h, path, session).Body.String()
		helpers, datastar := strings.Index(body, `src="/static/torrserver.js"`), strings.Index(body, `src="/static/datastar.js"`)
		if helpers < 0 || datastar < 0 || helpers > datastar {
			t.Errorf("%s loads torrserver.js at %d and datastar.js at %d, want the helpers first", path, helpers, datastar)
		}
	}
}

func TestSeriesSourcesSearchTheChosenSeasonAndRequireSignIn(t *testing.T) {
	var season string
	requests := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		season = r.URL.Query().Get("season")
		requests++
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	t.Cleanup(fake.Close)
	g := newGoogle(t)
	cfg := withTMDB(t, g.config())
	cfg.Sources.Torrents = jacred.NewClient(fake.URL, jacred.WithSearchAPI())
	h := web.New(cfg)
	for _, path := range []string{"/api/torrents?type=movie&id=438631", "/api/ts/selector"} {
		if res := get(t, h, path); res.Code != http.StatusUnauthorized {
			t.Errorf("signed out %s = %d, want 401", path, res.Code)
		}
	}
	session := signIn(t, h)
	res := getWith(t, h, "/api/torrents?type=tv&id=95396&season=2", session)
	if !strings.Contains(res.Body.String(), "Choose a season") || requests != 0 {
		t.Error("an unknown season reached JacRed rather than being explained")
	}
	res = getWith(t, h, "/api/torrents?type=tv&id=95396&season=1", session)
	if res.Code != http.StatusOK || season != "1" || !strings.Contains(res.Body.String(), "No releases found") {
		t.Errorf("season search = %d, season %q: %s", res.Code, season, res.Body)
	}
}

func TestJacRedFailureDoesNotSendCloudUsersToLocalSettings(t *testing.T) {
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_api_key"}`))
	}))
	t.Cleanup(fake.Close)
	g := newGoogle(t)
	cfg := withTMDB(t, g.config())
	cfg.Sources.Torrents = jacred.NewClient(fake.URL, jacred.WithSearchAPI(), jacred.WithAPIKey("jrs-server-key"))
	h := web.New(cfg)
	res := getWith(t, h, "/api/torrents?type=movie&id=438631", signIn(t, h))
	if strings.Contains(res.Body.String(), "Settings") || !strings.Contains(res.Body.String(), "JacRed is unavailable right now") {
		t.Fatalf("failed cloud search = %s, want a retry message without local settings", res.Body)
	}
}

// This stand-in rejects requests at Firestore's external gRPC boundary.
func TestAUserSeesAndCanRetryAFailedTorrServerSelector(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	firestorepb.RegisterFirestoreServer(server, &firestorepb.UnimplementedFirestoreServer{})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	t.Setenv("FIRESTORE_EMULATOR_HOST", listener.Addr().String())
	users, err := store.NewFirestore(context.Background(), "selector-test", "moviestracker")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })
	g := newGoogle(t)
	cfg := g.config()
	session := signIn(t, web.New(cfg))
	cfg.Store = users
	res := getWith(t, web.New(cfg), "/api/ts/selector", session)
	body := res.Body.String()
	for _, want := range []string{"datastar-patch-elements", `id="ts-source-selector"`, `role="alert"`, "Your TorrServers could not be loaded", "Retry", "/api/ts/selector", "$tsSelectorReady = false"} {
		if !strings.Contains(body, want) {
			t.Errorf("failed selector lacks %q: %s", want, body)
		}
	}
	if res.Code != http.StatusOK {
		t.Errorf("selector error status = %d, want a renderable SSE response", res.Code)
	}
}
