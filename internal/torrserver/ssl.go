package torrserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"time"
)

// ErrNoSSLAPI: the TorrServer predates MatriX.146, which added /ssl.
var ErrNoSSLAPI = errors.New("this TorrServer cannot manage its certificate (MatriX.146 or later can)")

// Where the certificate TorrServer serves HTTPS with comes from.
const (
	CertNone       = "none"        // HTTPS is off, or no certificate yet
	CertSelfSigned = "self-signed" // made and renewed by TorrServer
	CertUploaded   = "uploaded"    // uploaded through /ssl/upload
	CertFromFiles  = "user"        // files on its machine, by path
)

// SSLStatus is TorrServer's HTTPS: the startup flags it runs with and the
// certificate in use (GET /ssl/status).
type SSLStatus struct {
	Enabled     bool   `json:"enabled"` // started with --ssl
	Port        string `json:"port"`
	HTTPPort    string `json:"http_port"`
	HTTPEnabled bool   `json:"http_enabled"`
	ForceHTTPS  bool   `json:"force_https"`
	HTTPMedia   bool   `json:"http_media"`
	ReadOnly    bool   `json:"read_only"` // --rdb: no changes
	// CertFromFlags: --sslcert/--sslkey set the certificate on every start,
	// so it cannot be changed through the API.
	CertFromFlags bool     `json:"cert_from_flags"`
	Cert          CertInfo `json:"cert"`
}

// CertInfo describes a certificate; it never carries the key.
type CertInfo struct {
	Source    string    `json:"source"`
	CertFile  string    `json:"cert_file"`
	KeyFile   string    `json:"key_file"`
	Subject   string    `json:"subject"`
	Issuer    string    `json:"issuer"`
	DNSNames  []string  `json:"dns_names"`
	IPs       []string  `json:"ips"`
	NotBefore time.Time `json:"not_before"`
	NotAfter  time.Time `json:"not_after"`
	// Trusted: its chain verifies against the TorrServer machine's root CAs.
	Trusted bool `json:"trusted"`
	// Error says why the pair does not load, when it does not.
	Error string `json:"error"`
}

// Changeable reports whether the certificate can be changed through the API.
func (s SSLStatus) Changeable() bool {
	return s.Enabled && !s.ReadOnly && !s.CertFromFlags
}

// SSLStatus reads TorrServer's HTTPS mode and certificate.
func (c *Client) SSLStatus(ctx context.Context) (SSLStatus, error) {
	return c.sslCall(ctx, http.MethodGet, "/ssl/status", "", nil)
}

// UploadCertificate has TorrServer keep and serve a PEM certificate (with
// its chain) and unencrypted key, without a restart.
func (c *Client) UploadCertificate(ctx context.Context, certPEM, keyPEM []byte) (SSLStatus, error) {
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, part := range []struct {
		field, name string
		data        []byte
	}{{"cert", "cert.pem", certPEM}, {"key", "key.pem", keyPEM}} {
		if _, err := mw.CreateFormFile(part.field, part.name); err != nil {
			return SSLStatus{}, err
		}
		body.Write(part.data) // a part's writer appends to body
	}
	if err := mw.Close(); err != nil {
		return SSLStatus{}, err
	}
	return c.sslCall(ctx, http.MethodPost, "/ssl/upload", mw.FormDataContentType(), &body)
}

// UseCertificateFiles has TorrServer serve a certificate and key already on
// its machine (kept current by certbot or acme.sh, say); it picks up their
// renewals itself.
func (c *Client) UseCertificateFiles(ctx context.Context, certFile, keyFile string) (SSLStatus, error) {
	raw, err := json.Marshal(map[string]string{"cert": certFile, "key": keyFile})
	if err != nil {
		return SSLStatus{}, err
	}
	return c.sslCall(ctx, http.MethodPost, "/ssl/paths", "application/json", bytes.NewReader(raw))
}

// UseSelfSignedCertificate goes back to TorrServer's self-signed
// certificate, the one devices may already trust when there is one.
func (c *Client) UseSelfSignedCertificate(ctx context.Context) (SSLStatus, error) {
	return c.sslCall(ctx, http.MethodPost, "/ssl/selfsigned", "", nil)
}

// RegenerateCertificate makes a new self-signed certificate for the
// machine's current addresses.
func (c *Client) RegenerateCertificate(ctx context.Context) (SSLStatus, error) {
	return c.sslCall(ctx, http.MethodPost, "/ssl/regenerate", "", nil)
}

// Certificate downloads the certificate in use (never its key), with the
// file name TorrServer suggests.
func (c *Client) Certificate(ctx context.Context) ([]byte, string, error) {
	resp, err := c.sslDo(ctx, http.MethodGet, "/ssl/cert", "", nil)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	pem, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, "", err
	}
	name := "torrserver.crt"
	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil && params["filename"] != "" {
		name = params["filename"]
	}
	return pem, name, nil
}

// sslCall makes an /ssl request answered with the status.
func (c *Client) sslCall(ctx context.Context, method, path, contentType string, body io.Reader) (SSLStatus, error) {
	resp, err := c.sslDo(ctx, method, path, contentType, body)
	if err != nil {
		return SSLStatus{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	var st SSLStatus
	if err := decodeJSON(resp.Body, &st); err != nil {
		return SSLStatus{}, fmt.Errorf("torrserver %s: %w", path, err)
	}
	return st, nil
}

// sslDo sends an /ssl request; a refusal becomes TorrServer's own reason.
func (c *Client) sslDo(ctx context.Context, method, path, contentType string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusOK {
		return resp, nil
	}
	defer func() { _ = resp.Body.Close() }()
	var refusal struct {
		Error string `json:"error"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	switch {
	case resp.StatusCode == http.StatusNotFound && json.Unmarshal(raw, &refusal) != nil:
		return nil, ErrNoSSLAPI // no /ssl routes at all
	case json.Unmarshal(raw, &refusal) == nil && refusal.Error != "":
		return nil, errors.New(refusal.Error)
	default:
		return nil, fmt.Errorf("torrserver %s returned status %d", path, resp.StatusCode)
	}
}

// CertificateFiles names the certificate and key the TorrServer in use
// serves HTTPS with, for the gateway's HTTPS port to serve them too; an
// error when it serves no HTTPS or cannot say (before MatriX.146).
func (m *Manager) CertificateFiles(ctx context.Context) (cert, key string, err error) {
	st, err := m.Client().SSLStatus(ctx)
	if err != nil {
		return "", "", err
	}
	if !st.Enabled || st.Cert.CertFile == "" || st.Cert.KeyFile == "" {
		return "", "", errors.New("TorrServer serves no HTTPS")
	}
	return st.Cert.CertFile, st.Cert.KeyFile, nil
}

// CertExpiryWarning is how long before it expires a certificate is flagged:
// Let's Encrypt renews 30 days ahead, so 14 days left means a renewal failed.
const CertExpiryWarning = 14 * 24 * time.Hour

// Expired reports whether the certificate has expired.
func (c CertInfo) Expired(now time.Time) bool {
	return !c.NotAfter.IsZero() && !now.Before(c.NotAfter)
}

// ExpiresSoon reports whether the certificate expires within CertExpiryWarning.
func (c CertInfo) ExpiresSoon(now time.Time) bool {
	return !c.NotAfter.IsZero() && !c.Expired(now) && c.NotAfter.Sub(now) < CertExpiryWarning
}

// DaysLeft is how many whole days the certificate stays valid (0 when expired).
func (c CertInfo) DaysLeft(now time.Time) int {
	if c.Expired(now) {
		return 0
	}
	return int(c.NotAfter.Sub(now) / (24 * time.Hour))
}
