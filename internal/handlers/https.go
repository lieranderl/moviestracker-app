package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"
	"github.com/starfederation/datastar-go/datastar"
)

const (
	// maxPEMBytes bounds one uploaded certificate (chain included) or key.
	maxPEMBytes = 256 << 10
	// defaultSSLPort is TorrServer's HTTPS port when SslPort is 0.
	defaultSSLPort = 8091
	// certificateTimeout bounds one certificate change: TorrServer checks
	// the pair and serves it without a restart.
	certificateTimeout = 20 * time.Second
)

// httpsView describes TorrServer's HTTPS: for the managed engine, where
// other devices reach it and how they sign in; for any TorrServer, the
// certificate it serves (ssl is nil when it predates MatriX.146).
func (s *Server) httpsView(r *http.Request, sets torrserver.Fields, ssl *torrserver.SSLStatus, st views.SourceStatus) views.HTTPSView {
	v := views.HTTPSView{Managed: s.managed(), SSL: ssl, Now: time.Now(), PlainHTTP: plainHTTPFromElsewhere(r), Status: st}
	if !v.Managed {
		return v
	}
	startup := s.store.State().TorrServer.Startup
	port := sets.Int("SslPort")
	if port == 0 {
		port = defaultSSLPort
	}
	v.Serving, v.Reachable = startup.HTTPS, startup.HTTPS && startup.Reachable
	hosts := []string{"localhost"}
	if v.Reachable {
		hosts = networkAddresses()
		_, v.User, v.Password = s.engine.Endpoint()
	}
	for _, h := range hosts {
		v.Addresses = append(v.Addresses, "https://"+net.JoinHostPort(h, strconv.Itoa(port)))
	}
	return v
}

// sslStatus is TorrServer's certificate status, or nil when it has no /ssl
// API (before MatriX.146) or does not answer.
func sslStatus(ctx context.Context, client *torrserver.Client) *torrserver.SSLStatus {
	st, err := client.SSLStatus(ctx)
	if err != nil {
		return nil
	}
	return &st
}

// plainHTTPFromElsewhere reports whether r came over the network without
// TLS, so whatever the page uploads crosses it unencrypted.
func plainHTTPFromElsewhere(r *http.Request) bool {
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		return false
	}
	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	name := strings.ToLower(strings.Trim(host, "[]"))
	ip, ipErr := netip.ParseAddr(name)
	return name != "localhost" && !strings.HasSuffix(name, ".localhost") && (ipErr != nil || !ip.IsLoopback())
}

// networkAddresses are this computer's addresses on its networks.
func networkAddresses() []string {
	var out []string
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		if ip, ok := a.(*net.IPNet); ok && !ip.IP.IsLoopback() && !ip.IP.IsLinkLocalUnicast() && ip.IP.To4() != nil {
			out = append(out, ip.IP.String())
		}
	}
	if len(out) == 0 {
		if name, err := os.Hostname(); err == nil {
			out = append(out, name)
		}
	}
	return out
}

// patchHTTPS replaces the HTTPS card.
func (s *Server) patchHTTPS(w http.ResponseWriter, r *http.Request, ctx context.Context, st views.SourceStatus) {
	client := s.torrServer.Client()
	sets, err := client.Settings(ctx)
	if err != nil && st.OK {
		st = failed(engineAsleep)
	}
	v := s.httpsView(r, sets, sslStatus(ctx, client), st)
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.HTTPSCard(v)); err != nil {
		logSSEError(r, "patch HTTPS card", err)
	}
}

// changeCertificate makes one certificate change through TorrServer's /ssl
// API and shows the card with its outcome.
func (s *Server) changeCertificate(w http.ResponseWriter, r *http.Request, change func(*torrserver.Client, context.Context) (torrserver.SSLStatus, error), done string) {
	ctx, cancel := context.WithTimeout(r.Context(), certificateTimeout)
	defer cancel()
	st, err := change(s.torrServer.Client(), ctx)
	if err != nil {
		slog.Warn("TorrServer refused the HTTPS certificate change", "error", err)
		s.patchHTTPS(w, r, ctx, failed("TorrServer did not accept it: %v", err))
		return
	}
	s.forgetOldUpload(st.Cert.CertFile)
	s.patchHTTPS(w, r, ctx, succeeded(done))
}

// forgetOldUpload deletes a certificate and key Moviestracker kept for the
// engine before MatriX.146, once TorrServer serves another pair.
func (s *Server) forgetOldUpload(serving string) {
	if !s.managed() {
		return
	}
	old, _ := s.engine.CertificateFiles()
	if serving == "" || filepath.Clean(serving) == old {
		return
	}
	if err := s.engine.RemoveCertificate(); err != nil {
		slog.Warn("removing the old uploaded certificate failed", "error", err)
	}
}

// readPEM reads one uploaded PEM file of the certificate form.
func readPEM(r *http.Request, field string) ([]byte, error) {
	f, _, err := r.FormFile(field)
	if err != nil {
		return nil, err
	}
	defer func(f multipart.File) { _ = f.Close() }(f)
	return io.ReadAll(io.LimitReader(f, maxPEMBytes))
}

// handleUploadCertificate has TorrServer serve an uploaded certificate and key.
func (s *Server) handleUploadCertificate(w http.ResponseWriter, r *http.Request) {
	if s.adminAPI(w, r) == nil {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4*maxPEMBytes)
	if err := r.ParseMultipartForm(4 * maxPEMBytes); err != nil { // #nosec G120 -- the body is capped by MaxBytesReader above
		http.Error(w, "The certificate files are too large.", http.StatusRequestEntityTooLarge)
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	cert, certErr := readPEM(r, "sslCert")
	key, keyErr := readPEM(r, "sslKey")
	if certErr != nil || keyErr != nil {
		s.patchHTTPS(w, r, r.Context(), failed("Choose both the certificate and its private key."))
		return
	}
	s.changeCertificate(w, r, func(c *torrserver.Client, ctx context.Context) (torrserver.SSLStatus, error) { //nolint:revive // the method expression's order
		return c.UploadCertificate(ctx, cert, key)
	}, "Certificate uploaded. TorrServer serves it now.")
}

// handleCertificateFiles has TorrServer serve a certificate and key already
// on its machine.
func (s *Server) handleCertificateFiles(w http.ResponseWriter, r *http.Request) {
	if s.adminAPI(w, r) == nil {
		return
	}
	var in struct {
		Cert string `json:"httpsCertFile"`
		Key  string `json:"httpsKeyFile"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)).Decode(&in); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	in.Cert, in.Key = strings.TrimSpace(in.Cert), strings.TrimSpace(in.Key)
	if in.Cert == "" || in.Key == "" {
		s.patchHTTPS(w, r, r.Context(), failed("Give both the certificate file and the private key file."))
		return
	}
	s.changeCertificate(w, r, func(c *torrserver.Client, ctx context.Context) (torrserver.SSLStatus, error) { //nolint:revive // the method expression's order
		return c.UseCertificateFiles(ctx, in.Cert, in.Key)
	}, "TorrServer serves the certificate from these files now.")
}

// handleSelfSignedCertificate goes back to TorrServer's self-signed certificate.
func (s *Server) handleSelfSignedCertificate(w http.ResponseWriter, r *http.Request) {
	if s.adminAPI(w, r) == nil {
		return
	}
	s.changeCertificate(w, r, (*torrserver.Client).UseSelfSignedCertificate, "TorrServer serves its self-signed certificate now.")
}

// handleRegenerateCertificate has TorrServer make a new self-signed certificate.
func (s *Server) handleRegenerateCertificate(w http.ResponseWriter, r *http.Request) {
	if s.adminAPI(w, r) == nil {
		return
	}
	s.changeCertificate(w, r, (*torrserver.Client).RegenerateCertificate, "New self-signed certificate made. Browsers ask to accept it again.")
}

// handleDownloadCertificate gives the certificate TorrServer serves (never
// its key), to trust on a device.
func (s *Server) handleDownloadCertificate(w http.ResponseWriter, r *http.Request) {
	if s.adminAPI(w, r) == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), certificateTimeout)
	defer cancel()
	pem, name, err := s.torrServer.Client().Certificate(ctx)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, torrserver.ErrNoSSLAPI) {
			status = http.StatusNotFound
		}
		http.Error(w, i18n.Tf(r.Context(), "TorrServer has no certificate to give: %v", err), status)
		return
	}
	w.Header().Set("Content-Type", "application/x-x509-ca-cert")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(name)}))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(pem)
}
