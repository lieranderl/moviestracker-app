package torrserver_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

// sslEngine is a TorrServer (MatriX.146 or later) answering its /ssl API as
// upstream does: the status as JSON, and {"error": …} when it refuses.
type sslEngine struct {
	mu     sync.Mutex
	status string // the JSON /ssl/status and every change answer
	calls  []string
	upload map[string]string // form file → content of the last upload
	paths  map[string]string // body of the last /ssl/paths
	refuse int               // answer changes with this status, when set
}

func (e *sslEngine) serve(t *testing.T) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.calls = append(e.calls, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodPost && e.refuse != 0 {
			w.WriteHeader(e.refuse)
			_, _ = w.Write([]byte(`{"error":"the certificate is set by --sslcert/--sslkey"}`))
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /ssl/status", "POST /ssl/selfsigned", "POST /ssl/regenerate":
		case "GET /ssl/cert":
			w.Header().Set("Content-Type", "application/x-x509-ca-cert")
			w.Header().Set("Content-Disposition", `attachment; filename="torrserver.crt"`)
			_, _ = w.Write([]byte("-----BEGIN CERTIFICATE-----\n"))
			return
		case "POST /ssl/upload":
			e.upload = map[string]string{}
			for _, field := range []string{"cert", "key"} {
				f, _, err := r.FormFile(field)
				if err != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				raw, _ := io.ReadAll(f)
				e.upload[field] = string(raw)
			}
		case "POST /ssl/paths":
			_ = json.NewDecoder(r.Body).Decode(&e.paths)
		default:
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(e.status))
	}))
	t.Cleanup(ts.Close)
	return ts
}

// letsEncrypt is a MatriX.146 status: HTTPS on 8091 with files from certbot.
const letsEncrypt = `{"enabled":true,"port":"8091","http_port":"8090","http_enabled":true,"force_https":false,"http_media":false,"read_only":false,"cert_from_flags":false,
"cert":{"source":"user","cert_file":"/etc/letsencrypt/live/ts.example/fullchain.pem","key_file":"/etc/letsencrypt/live/ts.example/privkey.pem",
"subject":"CN=ts.example","issuer":"CN=R11,O=Let's Encrypt,C=US","dns_names":["ts.example"],"ips":["192.168.1.5"],
"not_before":"2026-09-01T00:00:00Z","not_after":"2026-11-30T00:00:00Z","trusted":true}}`

func TestTheClientReadsWhichCertificateTorrServerServesHTTPSWith(t *testing.T) {
	eng := &sslEngine{status: letsEncrypt}
	client := torrserver.NewClient(eng.serve(t).URL, nil)

	st, err := client.SSLStatus(context.Background())
	if err != nil {
		t.Fatalf("SSLStatus(): %v", err)
	}
	if !st.Enabled || st.Port != "8091" || st.HTTPPort != "8090" || !st.HTTPEnabled {
		t.Errorf("mode = %+v, want HTTPS on 8091 next to HTTP on 8090", st)
	}
	c := st.Cert
	if c.Source != torrserver.CertFromFiles || c.CertFile != "/etc/letsencrypt/live/ts.example/fullchain.pem" || c.KeyFile != "/etc/letsencrypt/live/ts.example/privkey.pem" {
		t.Errorf("source = %q from %q / %q, want certbot's files", c.Source, c.CertFile, c.KeyFile)
	}
	if c.Issuer != "CN=R11,O=Let's Encrypt,C=US" || !c.Trusted || len(c.DNSNames) != 1 || c.DNSNames[0] != "ts.example" || len(c.IPs) != 1 {
		t.Errorf("certificate = %+v, want a trusted Let's Encrypt one for ts.example", c)
	}
	if want := time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC); !c.NotAfter.Equal(want) {
		t.Errorf("NotAfter = %v, want %v", c.NotAfter, want)
	}
}

func TestAnOlderTorrServerHasNoCertificateAPI(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler()) // MatriX.145 and before
	t.Cleanup(ts.Close)
	_, err := torrserver.NewClient(ts.URL, nil).SSLStatus(context.Background())
	if !errors.Is(err, torrserver.ErrNoSSLAPI) {
		t.Errorf("SSLStatus() on MatriX.145 = %v, want ErrNoSSLAPI", err)
	}
}

func TestTheClientChangesTheCertificateWithoutRestartingTorrServer(t *testing.T) {
	eng := &sslEngine{status: letsEncrypt}
	client := torrserver.NewClient(eng.serve(t).URL, nil)
	ctx := context.Background()

	if _, err := client.UploadCertificate(ctx, []byte("CERT PEM"), []byte("KEY PEM")); err != nil {
		t.Fatalf("UploadCertificate(): %v", err)
	}
	if eng.upload["cert"] != "CERT PEM" || eng.upload["key"] != "KEY PEM" {
		t.Errorf("uploaded %v, want the certificate as cert and the key as key", eng.upload)
	}
	if _, err := client.UseCertificateFiles(ctx, "/etc/ts/cert.pem", "/etc/ts/key.pem"); err != nil {
		t.Fatalf("UseCertificateFiles(): %v", err)
	}
	if eng.paths["cert"] != "/etc/ts/cert.pem" || eng.paths["key"] != "/etc/ts/key.pem" {
		t.Errorf("paths sent = %v", eng.paths)
	}
	if st, err := client.UseSelfSignedCertificate(ctx); err != nil || !st.Enabled {
		t.Fatalf("UseSelfSignedCertificate() = %+v, %v", st, err)
	}
	if _, err := client.RegenerateCertificate(ctx); err != nil {
		t.Fatalf("RegenerateCertificate(): %v", err)
	}
	pem, name, err := client.Certificate(ctx)
	if err != nil || string(pem) != "-----BEGIN CERTIFICATE-----\n" || name != "torrserver.crt" {
		t.Errorf("Certificate() = %q, %q, %v", pem, name, err)
	}
	want := []string{"POST /ssl/upload", "POST /ssl/paths", "POST /ssl/selfsigned", "POST /ssl/regenerate", "GET /ssl/cert"}
	if len(eng.calls) != len(want) {
		t.Fatalf("calls = %v, want %v", eng.calls, want)
	}
	for i := range want {
		if eng.calls[i] != want[i] {
			t.Errorf("call %d = %q, want %q", i, eng.calls[i], want[i])
		}
	}
}

func TestTorrServersReasonForRefusingACertificateReachesTheUser(t *testing.T) {
	eng := &sslEngine{status: letsEncrypt, refuse: http.StatusConflict}
	_, err := torrserver.NewClient(eng.serve(t).URL, nil).UseSelfSignedCertificate(context.Background())
	if err == nil || err.Error() != "the certificate is set by --sslcert/--sslkey" {
		t.Errorf("error = %v, want TorrServer's own reason", err)
	}
}
