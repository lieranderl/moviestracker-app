package engine

import (
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// certDir is the folder under the engine directory holding an uploaded
// HTTPS certificate.
const certDir = "tls"

// SaveCertificate keeps an uploaded PEM certificate (with its chain) and
// private key in the engine folder, readable by its owner only, and returns
// their paths for TorrServer's SslCert and SslKey settings. A certificate
// that does not match its key is refused.
func (s *Supervisor) SaveCertificate(certPEM, keyPEM []byte) (certFile, keyFile string, err error) {
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		return "", "", fmt.Errorf("not a matching PEM certificate and private key: %w", err)
	}
	certFile, keyFile = s.CertificateFiles()
	if err := os.MkdirAll(filepath.Dir(certFile), 0o700); err != nil {
		return "", "", fmt.Errorf("create certificate folder: %w", err)
	}
	if err := writePrivate(keyFile, keyPEM); err != nil {
		return "", "", err
	}
	if err := writePrivate(certFile, certPEM); err != nil {
		return "", "", err
	}
	return certFile, keyFile, nil
}

// CertificateFiles are where an uploaded certificate and key are kept.
func (s *Supervisor) CertificateFiles() (certFile, keyFile string) {
	dir := filepath.Join(s.cfg.Dir, certDir)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
}

// RemoveCertificate deletes an uploaded certificate and key.
func (s *Supervisor) RemoveCertificate() error {
	err := os.RemoveAll(filepath.Join(s.cfg.Dir, certDir))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove certificate: %w", err)
	}
	return nil
}

// writePrivate replaces path with data, owner-only, so a reader never sees a
// half-written file.
func writePrivate(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".upload-*")
	if err != nil {
		return fmt.Errorf("save %s: %w", filepath.Base(path), err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("save %s: %w", filepath.Base(path), err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("save %s: %w", filepath.Base(path), err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("save %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("save %s: %w", filepath.Base(path), err)
	}
	return nil
}
