package engine_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/engine"
	"github.com/lieranderl/moviestracker-app/internal/engine/enginetest"
)

func TestAnUploadedCertificateIsKeptInTheEngineFolderForItsOwnerOnly(t *testing.T) {
	dir := t.TempDir()
	sup := engine.New(engine.Config{Binary: os.Args[0], Dir: dir})
	cert, key := enginetest.Certificate(t, "torrserver.example", time.Now().AddDate(0, 3, 0))

	certFile, keyFile, err := sup.SaveCertificate(cert, key)
	if err != nil {
		t.Fatalf("SaveCertificate(): %v", err)
	}
	for file, want := range map[string][]byte{certFile: cert, keyFile: key} {
		if !filepath.IsAbs(file) || !bytes.Equal(file2bytes(t, file), want) || filepath.Dir(filepath.Dir(file)) != dir {
			t.Errorf("%s is not the uploaded file in the engine folder", file)
		}
		if info, err := os.Stat(file); err == nil && runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600", file, info.Mode().Perm())
		}
	}

	if err := sup.RemoveCertificate(); err != nil {
		t.Fatalf("RemoveCertificate(): %v", err)
	}
	if _, err := os.Stat(certFile); !os.IsNotExist(err) {
		t.Errorf("certificate still there after removing it: %v", err)
	}
}

func TestACertificateIsRefusedWithAnotherKey(t *testing.T) {
	sup := engine.New(engine.Config{Binary: os.Args[0], Dir: t.TempDir()})
	cert, _ := enginetest.Certificate(t, "a.example", time.Now().AddDate(1, 0, 0))
	_, otherKey := enginetest.Certificate(t, "b.example", time.Now().AddDate(1, 0, 0))
	if _, _, err := sup.SaveCertificate(cert, otherKey); err == nil {
		t.Fatal("a certificate with someone else's key was accepted")
	}
	if _, _, err := sup.SaveCertificate([]byte("not pem"), otherKey); err == nil {
		t.Fatal("a file that is no certificate was accepted")
	}
}

func file2bytes(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path) // #nosec G304 -- test file
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return raw
}
