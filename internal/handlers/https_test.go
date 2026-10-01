package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"html"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/engine"
	"github.com/lieranderl/moviestracker-app/internal/engine/enginetest"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

// managedLocal is a Moviestracker running its own (fake) TorrServer.
func managedLocal(t *testing.T) (*local, *engine.Supervisor, *http.Cookie) {
	t.Helper()
	opt, sup := withEngine(t)
	l := newLocal(t, withAdmin(t), opt)
	admin := l.admin(t)
	l.action(t, "/api/settings/sources/torrserver", `{"torrserverMode":"managed"}`, admin)
	return l, sup, admin
}

// engineSettings reads the managed engine's BTSets.
func engineSettings(t *testing.T, sup *engine.Supervisor) torrserver.Fields {
	t.Helper()
	url, user, password := sup.Endpoint()
	sets, err := torrserver.NewClient(url, nil, torrserver.WithBasicAuth(user, password)).Settings(context.Background())
	if err != nil {
		t.Fatalf("read engine settings: %v", err)
	}
	return sets
}

// uploadCertificate posts the certificate form as the browser sends it.
func uploadCertificate(t *testing.T, l *local, cookie *http.Cookie, cert, key []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for field, content := range map[string][]byte{"sslCert": cert, "sslKey": key} {
		fw, _ := mw.CreateFormFile(field, field+".pem")
		_, _ = fw.Write(content)
	}
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/settings/https/certificate", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Datastar-Request", "true")
	return l.do(t, req, cookie)
}

func TestTheAdminCanServeTheManagedEngineOverHTTPSToOtherDevices(t *testing.T) {
	l, sup, admin := managedLocal(t)
	page := html.UnescapeString(l.do(t, httptest.NewRequest(http.MethodGet, "/settings/https", nil), admin).Body.String())
	if !strings.Contains(page, "Serve HTTPS") || !strings.Contains(page, "Reachable from other devices") {
		t.Fatalf("HTTPS settings page lacks the HTTPS switches")
	}

	rr := l.action(t, "/api/settings/engine/https", `{"https":{"HTTPS":false,"Reachable":true}}`, admin)
	if !strings.Contains(rr.Body.String(), "needs HTTPS") || l.store.State().TorrServer.Startup.Reachable {
		t.Fatalf("plain HTTP was opened to the network:\n%s", rr.Body.String())
	}

	before := sup.Status().PID
	rr = l.action(t, "/api/settings/engine/https", `{"https":{"HTTPS":true,"Reachable":true,"SslPort":18443}}`, admin)
	if st := l.store.State().TorrServer.Startup; !st.HTTPS || !st.Reachable {
		t.Fatalf("HTTPS not saved (%+v):\n%s", st, rr.Body.String())
	}
	if o := sup.Options(); !o.HTTPS || !o.Reachable || sup.Status().PID == before {
		t.Errorf("engine not restarted with HTTPS: %+v", o)
	}
	if got := engineSettings(t, sup).Int("SslPort"); got != 18443 {
		t.Errorf("SslPort = %d, want 18443", got)
	}

	page = html.UnescapeString(l.do(t, httptest.NewRequest(http.MethodGet, "/settings/https", nil), admin).Body.String())
	_, user, password := sup.Endpoint()
	if !strings.Contains(page, user) || !strings.Contains(page, password) || !strings.Contains(page, ":18443") {
		t.Errorf("page does not tell how other devices sign in to TorrServer")
	}
}

func TestChangingTheHTTPSPortRestartsAnEngineServingHTTPS(t *testing.T) {
	l, sup, admin := managedLocal(t)
	l.action(t, "/api/settings/engine/https", `{"https":{"HTTPS":true,"SslPort":18443}}`, admin)
	before := sup.Status().PID
	rr := l.action(t, "/api/settings/engine/https", `{"https":{"SslPort":18444}}`, admin)
	if sup.Status().PID == before || !strings.Contains(rr.Body.String(), "restarted") {
		t.Errorf("the new HTTPS port waits for a restart nobody asked for:\n%s", rr.Body.String())
	}
}

func TestAnUploadedCertificateServesTheManagedEnginesHTTPS(t *testing.T) {
	l, sup, admin := managedLocal(t)
	l.action(t, "/api/settings/engine/https", `{"https":{"HTTPS":true}}`, admin)
	cert, key := enginetest.Certificate(t, "torrserver.example", time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC))

	before := sup.Status().PID
	rr := uploadCertificate(t, l, admin, cert, key)
	sets := engineSettings(t, sup)
	certFile, keyFile := sets.String("SslCert"), sets.String("SslKey")
	if raw, err := os.ReadFile(certFile); err != nil || !bytes.Equal(raw, cert) { // #nosec G304 -- test path
		t.Fatalf("SslCert %q is not the uploaded certificate (%v):\n%s", certFile, err, rr.Body.String())
	}
	if raw, err := os.ReadFile(keyFile); err != nil || !bytes.Equal(raw, key) { // #nosec G304 -- test path
		t.Fatalf("SslKey %q is not the uploaded key (%v)", keyFile, err)
	}
	if sup.Status().PID == before {
		t.Errorf("engine serving HTTPS not restarted with the new certificate")
	}
	page := html.UnescapeString(l.do(t, httptest.NewRequest(http.MethodGet, "/settings/https", nil), admin).Body.String())
	if !strings.Contains(page, "torrserver.example") || !strings.Contains(page, "2027-03-01") {
		t.Errorf("page does not describe the certificate in use")
	}

	_, otherKey := enginetest.Certificate(t, "other.example", time.Now().AddDate(1, 0, 0))
	rr = uploadCertificate(t, l, admin, cert, otherKey)
	if !strings.Contains(rr.Body.String(), "do not match") || engineSettings(t, sup).String("SslKey") != keyFile {
		t.Errorf("a certificate with someone else's key was accepted:\n%s", rr.Body.String())
	}

	rr = l.action(t, "/api/settings/https/certificate/remove", `{}`, admin)
	if sets := engineSettings(t, sup); sets.String("SslCert") != "" || sets.String("SslKey") != "" {
		t.Errorf("removing the certificate left %q / %q:\n%s", sets.String("SslCert"), sets.String("SslKey"), rr.Body.String())
	}
	if _, err := os.Stat(certFile); !os.IsNotExist(err) {
		t.Errorf("removed certificate still on disk")
	}
}

func TestAnExternalTorrServerKeepsHTTPSPathsAndCannotBeStartedWithSSL(t *testing.T) {
	eng := newSettingsEngine(t, false)
	eng.btsets["SslPort"], eng.btsets["SslCert"], eng.btsets["SslKey"] = float64(8091), "", ""
	l := newLocal(t, withAdmin(t), withEngineAt(eng.URL))
	admin := l.admin(t)

	page := l.do(t, httptest.NewRequest(http.MethodGet, "/settings/https", nil), admin).Body.String()
	if !strings.Contains(page, "Only when Moviestracker runs TorrServer") || strings.Contains(page, "sslCert") {
		t.Errorf("an external TorrServer is offered HTTPS startup or upload")
	}
	l.action(t, "/api/settings/engine/https", `{"https":{"SslCert":"/etc/ssl/ts.pem","SslKey":"/etc/ssl/ts.key","SslPort":8091}}`, admin)
	if got := eng.lastSet(); got["SslCert"] != "/etc/ssl/ts.pem" || got["SslKey"] != "/etc/ssl/ts.key" {
		t.Errorf("certificate paths not saved: %v", got)
	}
	rr := l.action(t, "/api/settings/engine/https", `{"https":{"SslCert":"certs/ts.pem"}}`, admin)
	if !strings.Contains(rr.Body.String(), "full path") {
		t.Errorf("a relative certificate path was accepted:\n%s", rr.Body.String())
	}
	raw, _ := json.Marshal(eng.lastSet())
	if strings.Contains(string(raw), "certs/ts.pem") {
		t.Errorf("relative path reached TorrServer")
	}
}

func TestAnHTTPSSettingTorrServerCannotStartWithIsUndone(t *testing.T) {
	l, sup, admin := managedLocal(t)
	l.action(t, "/api/settings/engine/https", `{"https":{"HTTPS":true}}`, admin)

	rr := l.action(t, "/api/settings/engine/https", `{"https":{"SslCert":"/nowhere/cert.pem","SslKey":"/nowhere/key.pem"}}`, admin)
	if !strings.Contains(rr.Body.String(), "previous ones are back") {
		t.Errorf("a failed restart was not reported:\n%s", rr.Body.String())
	}
	if st := sup.Status(); st.State != engine.Running {
		t.Fatalf("engine left stopped after the failed restart: %+v", st)
	}
	if sets := engineSettings(t, sup); sets.String("SslCert") != "" || sets.String("SslKey") != "" {
		t.Errorf("the setting TorrServer could not start with was kept: %q / %q", sets.String("SslCert"), sets.String("SslKey"))
	}
	if o := sup.Options(); !o.HTTPS || !l.store.State().TorrServer.Startup.HTTPS {
		t.Errorf("HTTPS was not kept as it was: %+v", o)
	}
}
