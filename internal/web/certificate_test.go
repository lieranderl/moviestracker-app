package web_test

import (
	"html"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTheTorrServerPageAsksTheBrowserForTheCertificate(t *testing.T) {
	h, session, _ := torrServers(t)
	body := html.UnescapeString(getWith(t, h, "/torrserver", session).Body.String())
	for _, want := range []string{`id="ts-https"`, "tsSSL(el, $tsStatus.ok ? $tsSelected : '')", "@post('/api/ts/https'"} {
		if !strings.Contains(body, want) {
			t.Errorf("the TorrServer page lacks %q", want)
		}
	}
}

func TestTheWebAppShowsTheTorrServersCertificateReadOnly(t *testing.T) {
	h, session, _ := torrServers(t)
	// What the visitor's browser read from their TorrServer's /ssl/status.
	status := `{"status":{"enabled":true,"port":"8091","http_port":"8090","cert":{"source":"user",
"cert_file":"/etc/letsencrypt/live/ts.example/fullchain.pem","key_file":"/etc/letsencrypt/live/ts.example/privkey.pem",
"issuer":"CN=R11,O=Let's Encrypt,C=US","dns_names":["ts.example"],"trusted":true,
"not_before":"2026-09-01T00:00:00Z","not_after":"2099-11-30T00:00:00Z"}}}`
	res := sendSignals(t, h, http.MethodPost, "/api/ts/https", status, session)
	body := html.UnescapeString(res.Body.String())
	for _, want := range []string{`id="ts-https-details"`, "ts.example", "Let's Encrypt (R11)", "Trusted", "2099-11-30", "Files on the TorrServer machine"} {
		if !strings.Contains(body, want) {
			t.Errorf("POST /api/ts/https = %d lacks %q:\n%s", res.Code, want, body)
		}
	}
	for _, action := range []string{"Upload certificate", "/api/settings/https"} {
		if strings.Contains(body, action) {
			t.Errorf("the web app offers %q: it only shows the certificate", action)
		}
	}

	if body := sendSignals(t, h, http.MethodPost, "/api/ts/https", `{"status":null}`, session).Body.String(); strings.Contains(body, "Trusted") || !strings.Contains(body, `id="ts-https-details"`) {
		t.Errorf("without HTTPS the certificate is not cleared:\n%s", body)
	}
}

func TestTheCertificateIsShownOnlyToTheSignedIn(t *testing.T) {
	h, _, _ := torrServers(t)
	req := httptest.NewRequest(http.MethodPost, "/api/ts/https", strings.NewReader(`{"status":null}`))
	req.Header.Set("Datastar-Request", "true")
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Errorf("signed out = %d, want 401", res.Code)
	}
}
