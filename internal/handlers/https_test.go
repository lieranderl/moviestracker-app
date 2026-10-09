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
	"path/filepath"
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

func TestTheManagedEngineServesHTTPSOnThisComputerOnly(t *testing.T) {
	l, sup, admin := managedLocal(t)
	page := httpsPage(t, l, admin)
	if !strings.Contains(page, "Serve HTTPS") {
		t.Fatalf("HTTPS settings page lacks the HTTPS switch")
	}
	// Other devices come in through Other apps, with logins of their own;
	// TorrServer's port and login stay Moviestracker's.
	for _, gone := range []string{"Reachable from other devices", "HTTPS port"} {
		if strings.Contains(page, gone) {
			t.Errorf("the managed engine's HTTPS settings still offer %q", gone)
		}
	}

	before := sup.Status().PID
	rr := l.action(t, "/api/settings/engine/https", `{"https":{"HTTPS":true,"Reachable":true}}`, admin)
	if st := l.store.State().TorrServer.Startup; !st.HTTPS || st.Reachable {
		t.Fatalf("startup = %+v, want HTTPS and nothing opened to the network:\n%s", st, rr.Body.String())
	}
	if o := sup.Options(); !o.HTTPS || sup.Status().PID == before {
		t.Errorf("engine not restarted with HTTPS: %+v", o)
	}

	page = httpsPage(t, l, admin)
	_, _, password := sup.Endpoint()
	if strings.Contains(page, password) || !strings.Contains(page, "/settings/apps") {
		t.Errorf("the HTTPS page shows TorrServer's own password, or does not send other devices to Other apps")
	}
}

// httpsPage is the HTTPS settings page as the admin sees it.
func httpsPage(t *testing.T, l *local, admin *http.Cookie) string {
	t.Helper()
	return html.UnescapeString(l.do(t, httptest.NewRequest(http.MethodGet, "/settings/https", nil), admin).Body.String())
}

func TestAnUploadedCertificateIsServedWithoutRestartingTorrServer(t *testing.T) {
	l, sup, admin := managedLocal(t)
	l.action(t, "/api/settings/engine/https", `{"https":{"HTTPS":true}}`, admin)
	if page := httpsPage(t, l, admin); !strings.Contains(page, "Self-signed") || !strings.Contains(page, "Not trusted by the TorrServer machine") {
		t.Fatalf("page does not describe TorrServer's self-signed certificate")
	}
	cert, key := enginetest.Certificate(t, "torrserver.example", time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC))

	before := sup.Status().PID
	rr := html.UnescapeString(uploadCertificate(t, l, admin, cert, key).Body.String())
	certFile := engineSettings(t, sup).String("SslCert")
	if raw, err := os.ReadFile(certFile); err != nil || !bytes.Equal(raw, cert) { // #nosec G304 -- test path
		t.Fatalf("TorrServer does not serve the uploaded certificate (%q, %v):\n%s", certFile, err, rr)
	}
	if sup.Status().PID != before {
		t.Errorf("TorrServer restarted for a new certificate, dropping every stream")
	}
	if !strings.Contains(rr, "torrserver.example") || !strings.Contains(rr, "2027-03-01") || !strings.Contains(rr, "Uploaded") {
		t.Errorf("the card does not describe the uploaded certificate:\n%s", rr)
	}

	_, otherKey := enginetest.Certificate(t, "other.example", time.Now().AddDate(1, 0, 0))
	rr = uploadCertificate(t, l, admin, cert, otherKey).Body.String()
	if !strings.Contains(rr, "does not match") || engineSettings(t, sup).String("SslCert") != certFile {
		t.Errorf("a certificate with someone else's key was accepted:\n%s", rr)
	}
}

func TestTorrServerCanServeCertificateFilesThatCertbotRenews(t *testing.T) {
	l, sup, admin := managedLocal(t)
	l.action(t, "/api/settings/engine/https", `{"https":{"HTTPS":true}}`, admin)
	dir := t.TempDir()
	cert, key := enginetest.Certificate(t, "ts.example", time.Now().AddDate(0, 2, 0))
	certFile, keyFile := filepath.Join(dir, "fullchain.pem"), filepath.Join(dir, "privkey.pem")
	_ = os.WriteFile(certFile, cert, 0o600)
	_ = os.WriteFile(keyFile, key, 0o600)

	signals, _ := json.Marshal(map[string]string{"httpsCertFile": certFile, "httpsKeyFile": keyFile})
	rr := html.UnescapeString(l.action(t, "/api/settings/https/certificate/files", string(signals), admin).Body.String())
	if sets := engineSettings(t, sup); sets.String("SslCert") != certFile || sets.String("SslKey") != keyFile {
		t.Fatalf("TorrServer does not serve the files (%q / %q):\n%s", sets.String("SslCert"), sets.String("SslKey"), rr)
	}
	if !strings.Contains(rr, "Files on the TorrServer machine") || !strings.Contains(rr, certFile) || !strings.Contains(rr, "ts.example") {
		t.Errorf("the card does not show the files in use:\n%s", rr)
	}

	rr = l.action(t, "/api/settings/https/certificate/files", `{"httpsCertFile":"/nowhere/cert.pem","httpsKeyFile":"/nowhere/key.pem"}`, admin).Body.String()
	if !strings.Contains(rr, "TorrServer did not accept it") || engineSettings(t, sup).String("SslCert") != certFile {
		t.Errorf("files TorrServer cannot read were accepted:\n%s", rr)
	}
}

func TestTheAdminCanGoBackToANewSelfSignedCertificate(t *testing.T) {
	l, sup, admin := managedLocal(t)
	l.action(t, "/api/settings/engine/https", `{"https":{"HTTPS":true}}`, admin)
	selfSigned := engineSettings(t, sup).String("SslCert")
	cert, key := enginetest.Certificate(t, "torrserver.example", time.Now().AddDate(1, 0, 0))
	uploadCertificate(t, l, admin, cert, key)

	rr := l.action(t, "/api/settings/https/certificate/self-signed", `{}`, admin).Body.String()
	if got := engineSettings(t, sup).String("SslCert"); got != selfSigned {
		t.Fatalf("SslCert = %q, want the self-signed %q again:\n%s", got, selfSigned, rr)
	}
	before, _ := os.ReadFile(selfSigned) // #nosec G304 -- test path
	rr = l.action(t, "/api/settings/https/certificate/regenerate", `{}`, admin).Body.String()
	if after, _ := os.ReadFile(selfSigned); bytes.Equal(before, after) || !strings.Contains(rr, "New self-signed certificate") { // #nosec G304 -- test path
		t.Errorf("the self-signed certificate was not made again:\n%s", rr)
	}
}

func TestACertificateUploadedBeforeMatriX146IsDeletedOnceAnotherIsServed(t *testing.T) {
	l, sup, admin := managedLocal(t)
	l.action(t, "/api/settings/engine/https", `{"https":{"HTTPS":true}}`, admin)
	// Moviestracker kept uploads in the engine's tls folder before TorrServer could.
	legacyCert, legacyKey := sup.CertificateFiles()
	cert, key := enginetest.Certificate(t, "old.example", time.Now().AddDate(1, 0, 0))
	_ = os.MkdirAll(filepath.Dir(legacyCert), 0o700)
	_ = os.WriteFile(legacyCert, cert, 0o600)
	_ = os.WriteFile(legacyKey, key, 0o600)
	url, user, password := sup.Endpoint()
	if err := torrserver.NewClient(url, nil, torrserver.WithBasicAuth(user, password)).UpdateSettings(context.Background(), map[string]any{"SslCert": legacyCert, "SslKey": legacyKey}); err != nil {
		t.Fatal(err)
	}

	l.action(t, "/api/settings/https/certificate/self-signed", `{}`, admin)
	if _, err := os.Stat(legacyKey); !os.IsNotExist(err) {
		t.Errorf("the old uploaded key is still on disk: %v", err)
	}
}

func TestTheAdminCanDownloadTheCertificateForTheirDevices(t *testing.T) {
	l, _, admin := managedLocal(t)
	l.action(t, "/api/settings/engine/https", `{"https":{"HTTPS":true}}`, admin)

	rr := l.do(t, httptest.NewRequest(http.MethodGet, "/api/settings/https/certificate", nil), admin)
	if rr.Code != http.StatusOK || !strings.HasPrefix(rr.Body.String(), "-----BEGIN CERTIFICATE-----") || strings.Contains(rr.Body.String(), "PRIVATE KEY") {
		t.Fatalf("download = %d:\n%s", rr.Code, rr.Body.String())
	}
	if cd := rr.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, ".crt") {
		t.Errorf("Content-Disposition = %q, want a .crt attachment", cd)
	}
	if rr := l.do(t, httptest.NewRequest(http.MethodGet, "/api/settings/https/certificate", nil), nil); rr.Code == http.StatusOK {
		t.Errorf("the certificate was given to someone signed out")
	}
}

func TestWithoutHTTPSThereIsNoCertificateToChange(t *testing.T) {
	l, _, admin := managedLocal(t)
	page := httpsPage(t, l, admin)
	if !strings.Contains(page, "Turn on Serve HTTPS") || strings.Contains(page, "Upload certificate") {
		t.Errorf("certificate actions offered while TorrServer serves no HTTPS")
	}
}

func TestUploadingAKeyOverPlainHTTPFromAnotherDeviceIsWarnedAbout(t *testing.T) {
	l, _, admin := managedLocal(t)
	l.action(t, "/api/settings/engine/https", `{"https":{"HTTPS":true}}`, admin)
	const warning = "This page is open over plain HTTP"

	req := httptest.NewRequest(http.MethodGet, "/settings/https", nil)
	req.Host = "192.168.1.20:8080"
	if page := l.do(t, req, admin).Body.String(); !strings.Contains(page, warning) {
		t.Errorf("no warning that the private key would cross the network unencrypted")
	}
	req = httptest.NewRequest(http.MethodGet, "/settings/https", nil)
	req.Host = "localhost:8080"
	if page := l.do(t, req, admin).Body.String(); strings.Contains(page, warning) {
		t.Errorf("warned about plain HTTP on this very computer")
	}
}

func TestAnExternalTorrServersCertificateIsManagedFromTheCard(t *testing.T) {
	eng := newSettingsEngine(t, false)
	eng.ssl = `{"enabled":true,"port":"8091","http_port":"8090","http_enabled":true,
"cert":{"source":"user","cert_file":"/etc/letsencrypt/live/ts.example/fullchain.pem","key_file":"/etc/letsencrypt/live/ts.example/privkey.pem",
"subject":"CN=ts.example","issuer":"CN=R11,O=Let's Encrypt,C=US","dns_names":["ts.example"],"not_before":"2026-09-01T00:00:00Z","not_after":"2099-11-30T00:00:00Z","trusted":true}}`
	l := newLocal(t, withAdmin(t), withEngineAt(eng.URL))
	admin := l.admin(t)

	page := httpsPage(t, l, admin)
	for _, want := range []string{"ts.example", "Let's Encrypt", "Trusted by the TorrServer machine", "/etc/letsencrypt/live/ts.example/fullchain.pem", "Upload certificate"} {
		if !strings.Contains(page, want) {
			t.Errorf("the card lacks %q", want)
		}
	}
	if strings.Contains(page, "to the certificate's PEM private key") {
		t.Errorf("the unchecked key path field is still offered next to the card")
	}
	l.action(t, "/api/settings/https/certificate/self-signed", `{}`, admin)
	if got := strings.Join(eng.sslCalls, ","); !strings.Contains(got, "POST /ssl/selfsigned") {
		t.Errorf("TorrServer was not asked for its self-signed certificate: %s", got)
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
	if !strings.Contains(page, "MatriX.146 or later") {
		t.Errorf("the page does not say which TorrServer can manage its certificate")
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
	selfSigned := engineSettings(t, sup).String("SslCert") // made when HTTPS started

	rr := l.action(t, "/api/settings/engine/https", `{"https":{"SslCert":"/nowhere/cert.pem","SslKey":"/nowhere/key.pem"}}`, admin)
	if !strings.Contains(rr.Body.String(), "previous ones are back") {
		t.Errorf("a failed restart was not reported:\n%s", rr.Body.String())
	}
	if st := sup.Status(); st.State != engine.Running {
		t.Fatalf("engine left stopped after the failed restart: %+v", st)
	}
	if sets := engineSettings(t, sup); sets.String("SslCert") != selfSigned {
		t.Errorf("the setting TorrServer could not start with was kept: %q, want %q back", sets.String("SslCert"), selfSigned)
	}
	if o := sup.Options(); !o.HTTPS || !l.store.State().TorrServer.Startup.HTTPS {
		t.Errorf("HTTPS was not kept as it was: %+v", o)
	}
}
