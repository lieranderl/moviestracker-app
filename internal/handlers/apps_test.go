package handlers_test

import (
	"crypto/tls"
	"encoding/json"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/engine/enginetest"
	"github.com/lieranderl/moviestracker-app/internal/gateway"
	"github.com/lieranderl/moviestracker-app/internal/handlers"
)

// withAppsPort gives the server the gateway's ports (HTTP and HTTPS), on
// free ports of this machine, in front of the TorrServer the server uses.
func withAppsPort(t *testing.T, port **gateway.Port) localOption {
	var tlsPort *gateway.Port
	return withAppsPorts(t, "127.0.0.1:0", port, &tlsPort)
}

func withAppsPortAt(t *testing.T, addr string, port **gateway.Port) localOption {
	var tlsPort *gateway.Port
	return withAppsPorts(t, addr, port, &tlsPort)
}

func withAppsPorts(t *testing.T, addr string, port, tlsPort **gateway.Port) localOption {
	return func(c *handlers.Config) {
		torrServer := c.TorrServer
		gw := gateway.New(gateway.Config{
			Store: c.Store,
			Upstream: func() gateway.Upstream {
				url, user, password := torrServer.Endpoint()
				return gateway.Upstream{URL: url, User: user, Password: password}
			},
		})
		*port = gateway.NewPort(addr, gw)
		*tlsPort = gateway.NewPort("127.0.0.1:0", gw).WithTLS(gateway.NewCertificates(torrServer.CertificateFiles).Get)
		t.Cleanup(func() { _ = (*port).Close(); _ = (*tlsPort).Close() })
		c.AppsPort, c.AppsTLSPort = *port, *tlsPort
	}
}

// fromApp asks the gateway at port for path, as an app signed in as user.
func fromApp(t *testing.T, port *gateway.Port, path, user, password string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://"+port.Addr()+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if user != "" {
		req.SetBasicAuth(user, password)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s through the gateway: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestAnAdminOpensTorrServerToOtherApps(t *testing.T) {
	var port *gateway.Port
	engine := newFakeEngine(t, false)
	l := newLocal(t, withAdmin(t), withEngineAt(engine.URL), withAppsPort(t, &port))
	admin := l.admin(t)

	page := l.do(t, httpGet("/settings/apps"), admin)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Other apps") {
		t.Fatalf("GET /settings/apps = %d", page.Code)
	}
	if port.Addr() != "" {
		t.Fatal("the port is open before anyone turned it on")
	}

	rec := l.action(t, "/api/settings/apps", `{"appsOn":true,"appsInternet":false}`, admin)
	if !strings.Contains(rec.Body.String(), "is open") {
		t.Errorf("turning it on says:\n%s", rec.Body.String())
	}
	_, portNumber, _ := net.SplitHostPort(port.Addr())
	// The host the admin opened Moviestracker with, on the gateway's port.
	if want := "http://example.com:" + portNumber; !strings.Contains(rec.Body.String(), want) {
		t.Errorf("the page does not give apps the address %s:\n%s", want, rec.Body.String())
	}
	if st := l.store.State().Gateway; !st.Enabled || st.Internet {
		t.Errorf("saved gateway = %+v, want on for the home network", st)
	}
	if code, _ := fromApp(t, port, "/echo", "", ""); code != http.StatusUnauthorized {
		t.Errorf("the open gateway without a login = %d, want 401", code)
	}

	l.action(t, "/api/settings/apps", `{"appsOn":true,"appsInternet":true}`, admin)
	if !l.store.State().Gateway.Internet {
		t.Error("the internet switch was not saved")
	}

	addr := port.Addr()
	l.action(t, "/api/settings/apps", `{"appsOn":false,"appsInternet":false}`, admin)
	if l.store.State().Gateway.Enabled || port.Addr() != "" {
		t.Error("turning it off left it on")
	}
	if _, err := net.Dial("tcp", addr); err == nil {
		t.Error("the port still accepts connections")
	}
}

func TestAnAppGetsALoginWhosePasswordIsShownOnce(t *testing.T) {
	var port *gateway.Port
	engine := newFakeEngine(t, false)
	l := newLocal(t, withAdmin(t), withEngineAt(engine.URL), withAppsPort(t, &port))
	admin := l.admin(t)
	l.action(t, "/api/settings/apps", `{"appsOn":true,"appsInternet":false}`, admin)

	rec := l.action(t, "/api/settings/apps/logins", `{"appName":"Living room TV"}`, admin)
	body := rec.Body.String()
	password := regexp.MustCompile(`[2-9a-hj-km-np-z]{4}-[2-9a-hj-km-np-z]{4}-[2-9a-hj-km-np-z]{4}`).FindString(body)
	if password == "" || !strings.Contains(body, "livingroomtv") {
		t.Fatalf("the new login is not shown:\n%s", body)
	}
	logins := l.store.State().Gateway.Logins
	if len(logins) != 1 || logins[0].Name != "Living room TV" || logins[0].User != "livingroomtv" {
		t.Fatalf("saved logins = %+v", logins)
	}
	if strings.Contains(logins[0].PasswordHash, password) {
		t.Error("the password is saved as it is")
	}
	if code, body := fromApp(t, port, "/echo", "livingroomtv", password); code != http.StatusOK || body != "MatriX.145" {
		t.Errorf("the app with its new login: %d %q", code, body)
	}
	if page := l.do(t, httpGet("/settings/apps"), admin).Body.String(); strings.Contains(page, password) || !strings.Contains(page, "Living room TV") {
		t.Error("the settings page shows the password again, or not the app")
	}

	if rec := l.action(t, "/api/settings/apps/logins", `{"appName":"  "}`, admin); !strings.Contains(rec.Body.String(), "give the app a name") {
		t.Errorf("a login without a name: %s", rec.Body.String())
	}

	l.action(t, "/api/settings/apps/logins/livingroomtv/revoke", `{}`, admin)
	if len(l.store.State().Gateway.Logins) != 0 {
		t.Error("the revoked login is still saved")
	}
	if code, _ := fromApp(t, port, "/echo", "livingroomtv", password); code != http.StatusUnauthorized {
		t.Errorf("the revoked login still works: %d", code)
	}
}

func TestABusyPortIsExplainedAndTheGatewayStaysOff(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = busy.Close() })
	var port *gateway.Port
	l := newLocal(t, withAdmin(t), withAppsPortAt(t, busy.Addr().String(), &port))

	rec := l.action(t, "/api/settings/apps", `{"appsOn":true,"appsInternet":false}`, l.admin(t))
	if !strings.Contains(rec.Body.String(), "already uses port") {
		t.Errorf("turning it on with the port taken says:\n%s", rec.Body.String())
	}
	if l.store.State().Gateway.Enabled {
		t.Error("the gateway is saved as on although its port did not open")
	}
}

func TestTheGatewayOpensAgainWhenMoviestrackerStarts(t *testing.T) {
	var port *gateway.Port
	newLocal(t, func(store *config.Store) {
		addAccount(t, store, "admin", config.RoleAdmin)
		_ = store.Update(func(st *config.State) error { st.Gateway.Enabled = true; return nil })
	}, withAppsPort(t, &port))
	if port.Addr() == "" {
		t.Error("the gateway was on, but its port stayed shut after a restart")
	}
}

func TestOnlyAdminsManageOtherApps(t *testing.T) {
	var port *gateway.Port
	l := newLocal(t, func(store *config.Store) {
		addAccount(t, store, "admin", config.RoleAdmin)
		addAccount(t, store, "guest", config.RoleViewer)
	}, withAppsPort(t, &port))
	viewer := l.signIn(t, l.accounts.Lookup("guest"))
	if rec := l.do(t, httpGet("/settings/apps"), viewer); rec.Code != http.StatusForbidden {
		t.Errorf("a viewer opening Other apps: %d", rec.Code)
	}
	for _, path := range []string{"/api/settings/apps", "/api/settings/apps/logins", "/api/settings/apps/logins/x/revoke"} {
		if rec := l.action(t, path, `{"appsOn":true,"appName":"TV"}`, viewer); rec.Code != http.StatusForbidden {
			t.Errorf("a viewer posting %s: %d", path, rec.Code)
		}
	}
	if st := l.store.State().Gateway; st.Enabled || len(st.Logins) != 0 || port.Addr() != "" {
		t.Errorf("a viewer changed the gateway: %+v", st)
	}
}

func httpGet(path string) *http.Request { return httptest.NewRequest(http.MethodGet, path, nil) }

// fromAppOverHTTPS asks the gateway's HTTPS port for path without a login,
// and says which certificate it served.
func fromAppOverHTTPS(t *testing.T, port *gateway.Port, path string) (code int, served string) {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}} // #nosec G402 -- test: reads which certificate is served
	resp, err := client.Get("https://" + port.Addr() + path)
	if err != nil {
		t.Fatalf("GET %s over HTTPS through the gateway: %v", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode, resp.TLS.PeerCertificates[0].Subject.CommonName
}

// servingHTTPS makes engine serve HTTPS with a certificate for host, kept in
// files on this machine, as MatriX.146's /ssl/status names them.
func servingHTTPS(t *testing.T, engine *settingsEngine, host string) {
	t.Helper()
	dir := t.TempDir()
	certPEM, keyPEM := enginetest.Certificate(t, host, time.Now().AddDate(0, 3, 0))
	cert, key := filepath.Join(dir, "fullchain.pem"), filepath.Join(dir, "privkey.pem")
	_ = os.WriteFile(cert, certPEM, 0o600)
	_ = os.WriteFile(key, keyPEM, 0o600)
	raw, _ := json.Marshal(map[string]any{"enabled": true, "port": "18091", "http_port": "18090", "http_enabled": true,
		"cert": map[string]any{"source": "user", "cert_file": cert, "key_file": key, "dns_names": []string{host}}})
	engine.ssl = string(raw)
}

func TestOtherAppsReachTorrServerOverHTTPAndHTTPSWithTheirLogins(t *testing.T) {
	var port, tlsPort *gateway.Port
	engine := newSettingsEngine(t, false)
	servingHTTPS(t, engine, "nas.example")
	l := newLocal(t, withAdmin(t), withEngineAt(engine.URL), withAppsPorts(t, "127.0.0.1:0", &port, &tlsPort))
	admin := l.admin(t)

	body := l.action(t, "/api/settings/apps", `{"appsOn":true,"appsInternet":false}`, admin).Body.String()
	_, httpPort, _ := net.SplitHostPort(port.Addr())
	_, httpsPort, _ := net.SplitHostPort(tlsPort.Addr())
	for _, want := range []string{"http://example.com:" + httpPort, "https://example.com:" + httpsPort} {
		if !strings.Contains(body, want) {
			t.Errorf("the page does not give apps the address %s:\n%s", want, body)
		}
	}
	code, served := fromAppOverHTTPS(t, tlsPort, "/echo")
	if code != http.StatusUnauthorized || served != "nas.example" {
		t.Errorf("HTTPS without a login = %d with %q's certificate, want 401 with TorrServer's nas.example", code, served)
	}

	l.action(t, "/api/settings/apps", `{"appsOn":false,"appsInternet":false}`, admin)
	if tlsPort.Addr() != "" {
		t.Error("turning Other apps off left the HTTPS port open")
	}
}

func TestWithoutTorrServersHTTPSOtherAppsGetPlainHTTPAndAHint(t *testing.T) {
	var port, tlsPort *gateway.Port
	engine := newSettingsEngine(t, false)
	l := newLocal(t, withAdmin(t), withEngineAt(engine.URL), withAppsPorts(t, "127.0.0.1:0", &port, &tlsPort))
	body := html.UnescapeString(l.action(t, "/api/settings/apps", `{"appsOn":true,"appsInternet":false}`, l.admin(t)).Body.String())
	if strings.Contains(body, "https://example.com") || !strings.Contains(body, "Serve HTTPS") {
		t.Errorf("without TorrServer's HTTPS the page offers an HTTPS address or no way to get one:\n%s", body)
	}
}

func TestAnInstallReachableFromOtherDevicesNowLetsThemInThroughOtherApps(t *testing.T) {
	var port *gateway.Port
	l := newLocal(t, func(store *config.Store) {
		addAccount(t, store, "admin", config.RoleAdmin)
		_ = store.Update(func(st *config.State) error {
			st.TorrServer.Startup.HTTPS, st.TorrServer.Startup.Reachable = true, true
			return nil
		})
	}, withAppsPort(t, &port))
	if st := l.store.State(); !st.Gateway.Enabled || st.TorrServer.Startup.Reachable || !st.TorrServer.Startup.HTTPS {
		t.Errorf("after the update: gateway on = %t, engine reachable = %t, HTTPS = %t; want other devices to come in through Other apps over HTTPS",
			st.Gateway.Enabled, st.TorrServer.Startup.Reachable, st.TorrServer.Startup.HTTPS)
	}
	if port.Addr() == "" {
		t.Error("the gateway did not open for the devices that reached TorrServer before")
	}
}
