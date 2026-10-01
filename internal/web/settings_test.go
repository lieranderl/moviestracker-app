package web_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestUserCanRenderBrowserReadSettingsWithoutStartupOptions(t *testing.T) {
	h, session, _ := torrServers(t)
	res := postPlayer(t, h, "/api/ts/settings", `{"section":"streaming","url":"http://localhost:8090","nonce":1,"values":{"CacheSize":268435456,"ReaderReadAHead":90,"PreloadCache":20,"TrackTimecode":true}}`, session)
	if res.Code != http.StatusOK {
		t.Fatalf("settings = %d %s", res.Code, res.Body)
	}
	for _, want := range []string{"RAM cache", `tsSettingsValues.CacheSize`, `256`, "Saving makes TorrServer reconnect"} {
		if !strings.Contains(res.Body.String(), want) {
			t.Errorf("form lacks %q", want)
		}
	}
	if strings.Contains(res.Body.String(), "MaxSize") {
		t.Fatal("startup option offered")
	}
}

func TestSettingsAreValidatedBeforeBrowserWrites(t *testing.T) {
	h, session, _ := torrServers(t)
	for _, tc := range []struct{ name, body, want string }{
		{"valid", `{"section":"streaming","url":"http://localhost:8090","nonce":2,"values":{"CacheSize":256,"ReaderReadAHead":90}}`, `268435456`},
		{"range", `{"section":"streaming","values":{"CacheSize":2}}`, `must be between 32 and 16384 MB`},
		{"startup", `{"section":"streaming","values":{"MaxSize":10}}`, `unknown setting`},
		{"unknown", `{"section":"streaming","values":{"NewOption":7}}`, `unknown setting`},
		{"fraction", `{"section":"streaming","values":{"CacheSize":64.5}}`, `must be a whole number`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := postPlayer(t, h, "/api/ts/settings/validate", tc.body, session)
			if res.Code != 200 || !strings.Contains(res.Body.String(), tc.want) {
				t.Fatalf("validation = %d %s, want %s", res.Code, res.Body, tc.want)
			}
			if tc.name != "valid" && strings.Contains(res.Body.String(), "tsSettingsWrite(") {
				t.Fatal("invalid settings trigger a write")
			}
		})
	}
}

func TestBrowserSettingsEndpointsRequireSignInAndBoundRequests(t *testing.T) {
	h, session, _ := torrServers(t)
	for _, endpoint := range []string{"/api/ts/settings", "/api/ts/settings/validate"} {
		if res := postPlayer(t, h, endpoint, `{"section":"streaming","values":{}}`, nil); res.Code != 401 {
			t.Errorf("signed out = %d", res.Code)
		}
		if res := postPlayer(t, h, endpoint, `null`, session); res.Code != 400 {
			t.Errorf("null = %d", res.Code)
		}
		if res := postPlayer(t, h, endpoint, `{"section":"streaming","values":{"FriendlyName":"`+strings.Repeat("x", 129<<10)+`"}}`, session); res.Code != 400 {
			t.Errorf("oversized = %d", res.Code)
		}
	}
}
