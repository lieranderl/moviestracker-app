package handlers

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"
	"github.com/starfederation/datastar-go/datastar"
)

const (
	// maxPEMBytes bounds one uploaded certificate (chain included) or key.
	maxPEMBytes = 256 << 10
	// defaultSSLPort is TorrServer's HTTPS port when SslPort is 0.
	defaultSSLPort = 8091
	certMismatch   = "The certificate and private key do not match, or are not PEM files."
)

// httpsView describes the managed engine's HTTPS: where other devices reach
// it, how they sign in and which certificate it serves.
func (s *Server) httpsView(sets torrserver.Fields, st views.SourceStatus) views.HTTPSView {
	startup := s.store.State().TorrServer.Startup
	port := sets.Int("SslPort")
	if port == 0 {
		port = defaultSSLPort
	}
	v := views.HTTPSView{Serving: startup.HTTPS, Reachable: startup.HTTPS && startup.Reachable, Status: st}
	hosts := []string{"localhost"}
	if v.Reachable {
		hosts = networkAddresses()
		_, v.User, v.Password = s.engine.Endpoint()
	}
	for _, h := range hosts {
		v.Addresses = append(v.Addresses, "https://"+net.JoinHostPort(h, strconv.Itoa(port)))
	}
	uploaded, _ := s.engine.CertificateFiles()
	if path := sets.String("SslCert"); path != "" {
		v.Certificate = describeCertificate(path)
		v.Uploaded = filepath.Clean(path) == uploaded
	}
	return v
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

// describeCertificate reads the first certificate of a PEM file.
func describeCertificate(path string) *views.CertificateInfo {
	info := &views.CertificateInfo{Path: path}
	raw, err := os.ReadFile(path) // #nosec G304 -- the engine's own SslCert setting
	if err != nil {
		info.Problem = "The certificate file cannot be read."
		return info
	}
	block, _ := pem.Decode(raw)
	var cert *x509.Certificate
	if block != nil {
		cert, err = x509.ParseCertificate(block.Bytes)
	}
	if block == nil || err != nil {
		info.Problem = "The certificate file is not a PEM certificate."
		return info
	}
	info.Names = cert.DNSNames
	for _, ip := range cert.IPAddresses {
		info.Names = append(info.Names, ip.String())
	}
	if len(info.Names) == 0 && cert.Subject.CommonName != "" {
		info.Names = []string{cert.Subject.CommonName}
	}
	info.Expires = cert.NotAfter.UTC().Format(time.DateOnly)
	info.Expired = time.Now().After(cert.NotAfter)
	info.SelfSigned = cert.CheckSignatureFrom(cert) == nil
	return info
}

// patchHTTPS replaces the HTTPS card and the settings form, whose
// certificate paths changed with it.
func (s *Server) patchHTTPS(w http.ResponseWriter, r *http.Request, ctx context.Context, st views.SourceStatus) {
	sec, _ := sectionByID("https")
	v := s.engineSectionView(ctx, sec)
	sets, err := s.torrServer.Client().Settings(ctx)
	if err != nil {
		st = failed(engineAsleep)
	}
	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(views.HTTPSCard(s.httpsView(sets, st))); err != nil {
		logSSEError(r, "patch HTTPS card", err)
		return
	}
	if err := sse.PatchElementTempl(views.SettingsForm(v, views.SourceStatus{})); err != nil {
		logSSEError(r, "patch HTTPS settings", err)
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

// handleUploadCertificate makes the managed engine serve HTTPS with an
// uploaded certificate and key.
func (s *Server) handleUploadCertificate(w http.ResponseWriter, r *http.Request) {
	if s.adminAPI(w, r) == nil {
		return
	}
	if !s.managed() {
		http.Error(w, "Only when Moviestracker runs TorrServer.", http.StatusConflict)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4*maxPEMBytes)
	if err := r.ParseMultipartForm(4 * maxPEMBytes); err != nil { // #nosec G120 -- the body is capped by MaxBytesReader above
		http.Error(w, "The certificate files are too large.", http.StatusRequestEntityTooLarge)
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	cert, certErr := readPEM(r, "sslCert")
	key, keyErr := readPEM(r, "sslKey")
	if certErr != nil || keyErr != nil {
		s.patchHTTPS(w, r, ctx, failed("Choose both the certificate and its private key."))
		return
	}
	certFile, keyFile, err := s.engine.SaveCertificate(cert, key)
	if err != nil {
		s.patchHTTPS(w, r, ctx, failed(certMismatch))
		return
	}
	s.patchHTTPS(w, r, ctx, s.useCertificate(ctx, certFile, keyFile, "Certificate saved. TorrServer uses it once it serves HTTPS.", "Certificate saved. TorrServer restarted with it."))
}

// handleRemoveCertificate goes back to TorrServer's self-signed certificate.
func (s *Server) handleRemoveCertificate(w http.ResponseWriter, r *http.Request) {
	if s.adminAPI(w, r) == nil {
		return
	}
	if !s.managed() {
		http.Error(w, "Only when Moviestracker runs TorrServer.", http.StatusConflict)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	st := s.useCertificate(ctx, "", "", "Certificate removed. TorrServer makes a self-signed one.", "Certificate removed. TorrServer restarted with a self-signed one.")
	if st.OK {
		if err := s.engine.RemoveCertificate(); err != nil {
			slog.Warn("removing the HTTPS certificate failed", "error", err)
		}
	}
	s.patchHTTPS(w, r, ctx, st)
}

// useCertificate points TorrServer at a certificate ("" for a self-signed
// one) and restarts the engine when it serves HTTPS, as TorrServer reads the
// certificate when it starts. done and restarted report success either way.
func (s *Server) useCertificate(ctx context.Context, certFile, keyFile, done, restarted string) views.SourceStatus {
	client := s.torrServer.Client()
	current, err := client.Settings(ctx)
	if err != nil {
		return failed(engineAsleep)
	}
	previous := map[string]any{"SslCert": json.RawMessage(current["SslCert"]), "SslKey": json.RawMessage(current["SslKey"])}
	if err := client.UpdateSettings(ctx, map[string]any{"SslCert": certFile, "SslKey": keyFile}); err != nil {
		slog.Warn("saving the HTTPS certificate settings failed", "error", err)
		return failed("TorrServer did not accept the settings: %v", err)
	}
	startup := s.store.State().TorrServer.Startup
	if !startup.HTTPS {
		return succeeded(done)
	}
	if err := s.restartWithStartup(ctx, startup); err != nil {
		return s.recoverEngine(ctx, startup, previous, err)
	}
	return succeeded(restarted)
}
