package web_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/web"
)

// torrServers is a signed-in session in an app keeping users' data in users.
func torrServers(t *testing.T) (h http.Handler, session *httptest.ResponseRecorder, users *store.Memory) {
	t.Helper()
	g := newGoogle(t)
	cfg := g.config()
	users = store.NewMemory()
	cfg.Store = users
	h = web.New(cfg)
	return h, signIn(t, h), users
}

// sendSignals is a Datastar request carrying signals as Datastar sends
// them: in the ?datastar= query for GET and DELETE, else as the JSON body.
func sendSignals(t *testing.T, h http.Handler, method, path, signals string, session *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader = strings.NewReader(signals)
	if method == http.MethodGet || method == http.MethodDelete {
		path, body = path+"?datastar="+url.QueryEscape(signals), nil
	}
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Datastar-Request", "true")
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(sessionCookie(session))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestTheTorrServerPageListsTheUsersServers(t *testing.T) {
	h, session, users := torrServers(t)
	if _, err := users.SaveTorrServer(context.Background(), "1098765", store.TorrServer{Name: "Home", URL: "http://localhost:8090"}); err != nil {
		t.Fatal(err)
	}
	page := getWith(t, h, "/torrserver", session)
	body := page.Body.String()
	if page.Code != http.StatusOK || !strings.Contains(body, "http://localhost:8090") || !strings.Contains(body, "Home") {
		t.Fatalf("GET /torrserver = %d, want Ann's Home TorrServer listed", page.Code)
	}
	if !strings.Contains(body, `src="/static/torrserver.js"`) {
		t.Error("the TorrServer page does not load the browser's TorrServer client")
	}
	if !strings.Contains(getWith(t, h, "/", session).Body.String(), `href="/torrserver"`) {
		t.Error("the navbar does not link to the TorrServer page")
	}
}

func TestAUserCanAddALinkOrTorrentFileFromTheBrowser(t *testing.T) {
	h, session, _ := torrServers(t)
	page := getWith(t, h, "/torrserver", session)
	if page.Code != http.StatusOK {
		t.Fatalf("GET /torrserver = %d, want 200", page.Code)
	}
	for _, want := range []string{
		`id="torr-add-form"`, `name="addLinks"`, `name="addFiles"`, `multiple`, `name="addTitle"`,
		`data-on:submit="tsAddTorrent(`, `data-on:ts-added=`, `tsResetAdd(el, $tsSelected)`,
		`tsList($_list, $tsSelected, true)`,
		"One per line", "Up to 20", "Custom title", "Add to TorrServer", `id="torr-alert-container"`,
	} {
		if !strings.Contains(page.Body.String(), want) {
			t.Errorf("the browser add form lacks %q", want)
		}
	}
}

func TestAUserAddsATorrServer(t *testing.T) {
	h, session, users := torrServers(t)
	res := sendSignals(t, h, http.MethodPost, "/api/torrservers", `{"tsName":"NAS","tsUrl":" https://nas.example:8091/ "}`, session)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "https://nas.example:8091") {
		t.Fatalf("adding NAS = %d, want the list patched with it:\n%s", res.Code, res.Body)
	}
	list, _ := users.TorrServers(context.Background(), "1098765")
	if len(list) != 1 || list[0].Name != "NAS" || list[0].URL != "https://nas.example:8091" {
		t.Errorf("Ann's TorrServers = %+v, want NAS at https://nas.example:8091", list)
	}
}

func TestATorrServerAddressMustBeAWebAddress(t *testing.T) {
	h, session, users := torrServers(t)
	res := sendSignals(t, h, http.MethodPost, "/api/torrservers", `{"tsName":"x","tsUrl":"nas:8090"}`, session)
	if !strings.Contains(res.Body.String(), "tsFormError") {
		t.Errorf("adding nas:8090 = %q, want an error for the form", res.Body.String())
	}
	if list, _ := users.TorrServers(context.Background(), "1098765"); len(list) != 0 {
		t.Errorf("TorrServers = %+v, want none", list)
	}
}

func TestAUserRemovesATorrServer(t *testing.T) {
	h, session, users := torrServers(t)
	home, _ := users.SaveTorrServer(context.Background(), "1098765", store.TorrServer{Name: "Home", URL: "http://localhost:8090"})
	res := sendSignals(t, h, http.MethodDelete, "/api/torrservers/"+home.ID, `{}`, session)
	if res.Code != http.StatusOK || strings.Contains(res.Body.String(), `value="http://localhost:8090"`) {
		t.Errorf("removing Home = %d, want the list patched without it", res.Code)
	}
	if list, _ := users.TorrServers(context.Background(), "1098765"); len(list) != 0 {
		t.Errorf("TorrServers after removing = %+v, want none", list)
	}
}

func TestTorrServersNeedASignedInUser(t *testing.T) {
	h, _, _ := torrServers(t)
	if res := get(t, h, "/torrserver"); res.Code != http.StatusSeeOther {
		t.Errorf("GET /torrserver signed out = %d, want 303", res.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/torrservers", strings.NewReader(`{"tsUrl":"http://x"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("POST /api/torrservers signed out = %d, want 401", rec.Code)
	}
}

func TestRemovingThePickedTorrServerPicksAnotherAndForgetsItsLogin(t *testing.T) {
	h, session, users := torrServers(t)
	ctx := context.Background()
	home, _ := users.SaveTorrServer(ctx, "1098765", store.TorrServer{Name: "Home", URL: "http://localhost:8090"})
	if _, err := users.SaveTorrServer(ctx, "1098765", store.TorrServer{Name: "NAS", URL: "https://nas.example:8091"}); err != nil {
		t.Fatal(err)
	}
	res := sendSignals(t, h, http.MethodDelete, "/api/torrservers/"+home.ID, `{"tsSelected":"http://localhost:8090"}`, session).Body.String()
	if !strings.Contains(res, `"tsForget":"http://localhost:8090"`) {
		t.Errorf("removing Home does not tell the browser to forget its login:\n%s", res)
	}
	if !strings.Contains(res, `"tsSelected":"https://nas.example:8091"`) {
		t.Errorf("removing the picked Home does not pick NAS:\n%s", res)
	}
}

func TestRemovingATorrServerThatIsNotPickedKeepsThePick(t *testing.T) {
	h, session, users := torrServers(t)
	ctx := context.Background()
	home, _ := users.SaveTorrServer(ctx, "1098765", store.TorrServer{Name: "Home", URL: "http://localhost:8090"})
	res := sendSignals(t, h, http.MethodDelete, "/api/torrservers/"+home.ID, `{"tsSelected":"https://other.example"}`, session).Body.String()
	if strings.Contains(res, `"tsSelected"`) {
		t.Errorf("removing Home changed the pick, which was another server:\n%s", res)
	}
}

func TestBrowserBatchesReportPartialProgressWithTheAppAlert(t *testing.T) {
	h, session, _ := torrServers(t)
	res := postPlayer(t, h, "/api/ts/add-result", `{"added":2,"refused":["invalid.txt"],"failed":["failed.torrent"]}`, session)
	for _, want := range []string{`id="torr-alert-container"`, "Added 2 torrents to TorrServer.", "invalid.txt", "TorrServer could not add: failed.torrent."} {
		if !strings.Contains(res.Body.String(), want) {
			t.Errorf("batch result lacks %q: %s", want, res.Body)
		}
	}
}
