package gateway_test

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/engine/enginetest"
	"github.com/lieranderl/moviestracker-app/internal/gateway"
)

// writePair writes a certificate for host and its key into dir.
func writePair(t *testing.T, dir, host string, modified time.Time) (cert, key string) {
	t.Helper()
	certPEM, keyPEM := enginetest.Certificate(t, host, time.Now().AddDate(0, 3, 0))
	cert, key = filepath.Join(dir, host+".pem"), filepath.Join(dir, host+".key")
	for path, data := range map[string][]byte{cert: certPEM, key: keyPEM} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, modified, modified); err != nil {
			t.Fatal(err)
		}
	}
	return cert, key
}

// servedName is the name on the certificate an HTTPS port serves, over a new
// connection; "" when the handshake fails.
func servedName(t *testing.T, addr string) string {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig:   &tls.Config{InsecureSkipVerify: true}, // #nosec G402 -- test: reads which certificate is served
		DisableKeepAlives: true,
	}}
	resp, err := client.Get("https://" + addr + "/")
	if err != nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()
	if body, _ := io.ReadAll(resp.Body); string(body) != "gateway" {
		t.Errorf("HTTPS port answered %q", body)
	}
	return resp.TLS.PeerCertificates[0].Subject.CommonName
}

// pairs is TorrServer's certificate files as /ssl/status names them.
type pairs struct {
	mu        sync.Mutex
	cert, key string
}

func (p *pairs) set(cert, key string) { p.mu.Lock(); p.cert, p.key = cert, key; p.mu.Unlock() }

func (p *pairs) files(context.Context) (string, string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cert == "" {
		return "", "", errors.New("TorrServer serves no HTTPS")
	}
	return p.cert, p.key, nil
}

func TestAppsReachTorrServerOverHTTPSWithTheCertificateTorrServerServes(t *testing.T) {
	dir := t.TempDir()
	start := time.Now().Add(-time.Hour)
	var torrServer pairs
	torrServer.set(writePair(t, dir, "nas.example", start))
	now := start
	certs := gateway.NewCertificates(torrServer.files, gateway.WithClock(func() time.Time { return now }))
	hello := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "gateway") })
	port := gateway.NewPort("127.0.0.1:0", hello).WithTLS(certs.Get)
	t.Cleanup(func() { _ = port.Close() })
	if err := port.Open(); err != nil {
		t.Fatal(err)
	}

	if got := servedName(t, port.Addr()); got != "nas.example" {
		t.Errorf("served %q, want TorrServer's nas.example", got)
	}

	// certbot renews the same files: served at once, without a restart.
	certPEM, keyPEM := enginetest.Certificate(t, "renewed.example", time.Now().AddDate(0, 3, 0))
	cert, key, _ := torrServer.files(context.Background())
	for path, data := range map[string][]byte{cert: certPEM, key: keyPEM} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := servedName(t, port.Addr()); got != "renewed.example" {
		t.Errorf("after renewal served %q, want renewed.example", got)
	}

	// An upload in Settings → HTTPS: TorrServer names other files.
	torrServer.set(writePair(t, dir, "uploaded.example", start))
	now = now.Add(time.Minute)
	if got := servedName(t, port.Addr()); got != "uploaded.example" {
		t.Errorf("after an upload served %q, want uploaded.example", got)
	}
}

func TestWithoutTorrServersCertificateTheHTTPSPortServesNothing(t *testing.T) {
	var torrServer pairs
	port := gateway.NewPort("127.0.0.1:0", http.NotFoundHandler()).WithTLS(gateway.NewCertificates(torrServer.files).Get)
	t.Cleanup(func() { _ = port.Close() })
	if err := port.Open(); err != nil {
		t.Fatal(err)
	}
	if got := servedName(t, port.Addr()); got != "" {
		t.Errorf("served %q without a certificate", got)
	}
}
