package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// certDir is the folder under the engine directory where Moviestracker kept
// an uploaded HTTPS certificate before TorrServer could (MatriX.146).
const certDir = "tls"

// CertificateFiles are where a certificate uploaded before MatriX.146 and
// its key are kept.
func (s *Supervisor) CertificateFiles() (certFile, keyFile string) {
	dir := filepath.Join(s.cfg.Dir, certDir)
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
}

// RemoveCertificate deletes a certificate and key uploaded before MatriX.146.
func (s *Supervisor) RemoveCertificate() error {
	s.certMu.Lock()
	defer s.certMu.Unlock()
	err := os.RemoveAll(filepath.Join(s.cfg.Dir, certDir))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove certificate: %w", err)
	}
	return nil
}
