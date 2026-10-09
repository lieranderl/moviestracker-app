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
	for _, want := range []string{`id="ts-https"`, "tsSSL(el, $tsStatus.ok ? $tsSelected : '')", "evt.detail.url === $tsSelected && @post('/api/ts/https', {payload: evt.detail})", "data-on:ts-ssl-change"} {
		if !strings.Contains(body, want) {
			t.Errorf("the TorrServer page lacks %q", want)
		}
	}
}

// certificateStatus is what the visitor's browser read from their
// TorrServer's /ssl/status, with the address it read it from.
func certificateStatus(url, extra string) string {
	return `{"url":"` + url + `","status":{"enabled":true,"port":"8091","http_port":"8090",` + extra + `"cert":{"source":"user",
"cert_file":"/etc/letsencrypt/live/ts.example/fullchain.pem","key_file":"/etc/letsencrypt/live/ts.example/privkey.pem",
"issuer":"CN=R11,O=Let's Encrypt,C=US","dns_names":["ts.example"],"trusted":true,
"not_before":"2026-09-01T00:00:00Z","not_after":"2099-11-30T00:00:00Z"}}}`
}

func TestTheWebAppShowsTheTorrServersCertificate(t *testing.T) {
	h, session, _ := torrServers(t)
	res := sendSignals(t, h, http.MethodPost, "/api/ts/https", certificateStatus("https://ts.example:8091", ""), session)
	body := html.UnescapeString(res.Body.String())
	for _, want := range []string{`id="ts-https-details"`, "ts.example", "Let's Encrypt (R11)", "Trusted by the TorrServer machine", "2099-11-30", "Files on the TorrServer machine"} {
		if !strings.Contains(body, want) {
			t.Errorf("POST /api/ts/https = %d lacks %q:\n%s", res.Code, want, body)
		}
	}

	if body := sendSignals(t, h, http.MethodPost, "/api/ts/https", `{"status":null}`, session).Body.String(); strings.Contains(body, "Trusted") || !strings.Contains(body, `id="ts-https-details"`) {
		t.Errorf("without HTTPS the certificate is not cleared:\n%s", body)
	}
}

func TestAUserChangesTheirTorrServersCertificateFromTheirBrowser(t *testing.T) {
	h, session, _ := torrServers(t)
	body := html.UnescapeString(sendSignals(t, h, http.MethodPost, "/api/ts/https", certificateStatus("https://ts.example:8091", ""), session).Body.String())
	for _, want := range []string{
		"Upload certificate", `name="sslCert"`, `name="sslKey"`, "tsSSLChange(el.closest('#ts-https'), $tsSelected, 'upload', {cert: el.elements.sslCert.files[0], key: el.elements.sslKey.files[0]})",
		"Use these files", "tsSSLChange(el.closest('#ts-https'), $tsSelected, 'paths', {cert: $httpsCertFile, key: $httpsKeyFile})",
		"Use the self-signed certificate", "tsSSLChange(el.closest('#ts-https'), $tsSelected, 'selfsigned')",
		"Download the certificate", "tsSSLDownload($tsSelected)",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the web app's certificate lacks %q:\n%s", want, body)
		}
	}
	// The key goes from the browser to TorrServer, never through Moviestracker.
	if strings.Contains(body, "/api/settings/https") {
		t.Errorf("the web app sends the certificate through the server:\n%s", body)
	}
	if strings.Contains(body, "plain HTTP") {
		t.Errorf("an HTTPS TorrServer is warned about as plain HTTP:\n%s", body)
	}
}

func TestACertificateSetAtStartupIsShownLockedButCanStillBeDownloaded(t *testing.T) {
	h, session, _ := torrServers(t)
	for extra, note := range map[string]string{
		`"cert_from_flags":true,`: "--sslcert and --sslkey",
		`"read_only":true,`:       "read-only (--rdb)",
	} {
		body := html.UnescapeString(sendSignals(t, h, http.MethodPost, "/api/ts/https", certificateStatus("http://192.168.1.5:8090", extra), session).Body.String())
		for _, want := range []string{note, "<fieldset disabled", "Upload certificate", "tsSSLDownload($tsSelected)"} {
			if !strings.Contains(body, want) {
				t.Errorf("with %s the certificate lacks %q:\n%s", extra, want, body)
			}
		}
		if strings.Contains(body, "plain HTTP") {
			t.Errorf("with %s nothing can be uploaded, yet the plain-HTTP warning shows", extra)
		}
	}
}

func TestTheCertificateNamesTheProtocolsAndPortsTorrServerServes(t *testing.T) {
	h, session, _ := torrServers(t)
	for extra, want := range map[string]string{
		`"http_enabled":true,`:  "HTTPS on port 8091, HTTP on port 8090",
		`"http_enabled":false,`: "HTTPS only, on port 8091",
	} {
		if body := html.UnescapeString(sendSignals(t, h, http.MethodPost, "/api/ts/https", certificateStatus("https://ts.example:8091", extra), session).Body.String()); !strings.Contains(body, want) {
			t.Errorf("with %s the certificate lacks %q", extra, want)
		}
	}
}

func TestTheWebAppWarnsBeforeAKeyCrossesTheNetworkUnencrypted(t *testing.T) {
	h, session, _ := torrServers(t)
	for url, warned := range map[string]bool{
		"http://192.168.1.5:8090": true,
		"http://localhost:8090":   false,
		"http://127.0.0.1:8090":   false,
		"https://ts.example:8091": false,
	} {
		body := sendSignals(t, h, http.MethodPost, "/api/ts/https", certificateStatus(url, ""), session).Body.String()
		if got := strings.Contains(body, "plain HTTP"); got != warned {
			t.Errorf("TorrServer at %s: plain-HTTP warning = %t, want %t", url, got, warned)
		}
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
