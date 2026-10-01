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
// that does not match its key is refused; on any failure the previous pair
// stays as it was.
func (s *Supervisor) SaveCertificate(certPEM, keyPEM []byte) (certFile, keyFile string, err error) {
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		return "", "", fmt.Errorf("not a matching PEM certificate and private key: %w", err)
	}
	s.certMu.Lock()
	defer s.certMu.Unlock()
	certFile, keyFile = s.CertificateFiles()
	if err := os.MkdirAll(filepath.Dir(certFile), 0o700); err != nil {
		return "", "", fmt.Errorf("create certificate folder: %w", err)
	}
	if err := replacePair(map[string][]byte{certFile: certPEM, keyFile: keyPEM}); err != nil {
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
	s.certMu.Lock()
	defer s.certMu.Unlock()
	err := os.RemoveAll(filepath.Join(s.cfg.Dir, certDir))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove certificate: %w", err)
	}
	return nil
}

// replacePair replaces each file with its data, owner-only. Both are
// written in full before either is replaced, and a failure puts back the
// files that were there, so the certificate and key always match.
func replacePair(files map[string][]byte) (err error) {
	staged := map[string]string{} // file -> its new content, written aside
	defer func() {
		for _, tmp := range staged {
			_ = os.Remove(tmp)
		}
	}()
	for file, data := range files {
		tmp, err := writeAside(file, data)
		if err != nil {
			return err
		}
		staged[file] = tmp
	}
	backups := map[string]string{} // file -> what it held before
	defer func() {
		for file, old := range backups {
			if err != nil {
				_ = os.Rename(old, file)
			} else {
				_ = os.Remove(old)
			}
		}
	}()
	for file, tmp := range staged {
		old := file + ".previous"
		switch err := os.Rename(file, old); {
		case err == nil:
			backups[file] = old
		case !errors.Is(err, os.ErrNotExist):
			return fmt.Errorf("save %s: %w", filepath.Base(file), err)
		}
		if err := os.Rename(tmp, file); err != nil {
			if _, kept := backups[file]; !kept {
				_ = os.Remove(file) // nothing to put back: leave no half pair
			}
			return fmt.Errorf("save %s: %w", filepath.Base(file), err)
		}
		delete(staged, file)
	}
	return nil
}

// writeAside writes data to a new owner-only file next to path.
func writeAside(path string, data []byte) (string, error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".upload-*")
	if err != nil {
		return "", fmt.Errorf("save %s: %w", filepath.Base(path), err)
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return "", fmt.Errorf("save %s: %w", filepath.Base(path), errors.Join(werr, cerr))
	}
	return tmp.Name(), nil
}
