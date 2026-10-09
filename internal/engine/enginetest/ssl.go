package enginetest

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// fakeSSL is MatriX.146's certificate API (/ssl/*) for the certificate in
// the SslCert and SslKey settings, which get and set read and save.
type fakeSSL struct {
	enabled           bool // started with --ssl
	httpPort, dir     string
	get               func() map[string]any
	set               func(cert, key string)
	authorized        func(*http.Request) bool
	selfCert, selfKey string
	upCert, upKey     string
}

func newFakeSSL(enabled bool, dir, httpPort string, get func() map[string]any, set func(cert, key string), authorized func(*http.Request) bool) *fakeSSL {
	root, _ := filepath.Abs(dir)
	return &fakeSSL{
		enabled: enabled, httpPort: httpPort, dir: root, get: get, set: set, authorized: authorized,
		selfCert: filepath.Join(root, "server.pem"), selfKey: filepath.Join(root, "server.key"),
		upCert: filepath.Join(root, "ssl", "uploaded.crt"), upKey: filepath.Join(root, "ssl", "uploaded.key"),
	}
}

// start makes the self-signed pair, as TorrServer does when it starts with
// --ssl and no certificate.
func (f *fakeSSL) start() error {
	if cert, _ := f.paths(); !f.enabled || cert != "" {
		return nil
	}
	if err := f.makeSelfSigned(); err != nil {
		return err
	}
	f.set(f.selfCert, f.selfKey)
	return nil
}

func (f *fakeSSL) paths() (cert, key string) {
	sets := f.get()
	cert, _ = sets["SslCert"].(string)
	key, _ = sets["SslKey"].(string)
	return cert, key
}

func (f *fakeSSL) makeSelfSigned() error {
	cert, key, err := makeCertificate("localhost", time.Now().AddDate(1, 0, 0))
	if err != nil {
		return err
	}
	if err := os.WriteFile(f.selfCert, cert, 0o600); err != nil {
		return err
	}
	return os.WriteFile(f.selfKey, key, 0o600)
}

func (f *fakeSSL) routes(mux *http.ServeMux) {
	handle := func(pattern string, change bool, h func(http.ResponseWriter, *http.Request) error) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if !f.authorized(r) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if change && !f.enabled {
				refuse(w, http.StatusConflict, "HTTPS is not enabled (start TorrServer with --ssl)")
				return
			}
			if err := h(w, r); err != nil {
				refuse(w, http.StatusBadRequest, err.Error())
				return
			}
			if r.URL.Path != "/ssl/cert" {
				_ = json.NewEncoder(w).Encode(f.status())
			}
		})
	}
	handle("GET /ssl/status", false, func(http.ResponseWriter, *http.Request) error { return nil })
	handle("GET /ssl/cert", false, func(w http.ResponseWriter, r *http.Request) error {
		cert, _ := f.paths()
		raw, err := os.ReadFile(cert) // #nosec G304 -- test fake: the configured certificate
		if !f.enabled || err != nil {
			refuse(w, http.StatusNotFound, "no certificate configured")
			return nil
		}
		w.Header().Set("Content-Type", "application/x-x509-ca-cert")
		w.Header().Set("Content-Disposition", `attachment; filename="torrserver.crt"`)
		_, _ = w.Write(raw)
		return nil
	})
	handle("POST /ssl/upload", true, func(w http.ResponseWriter, r *http.Request) error {
		cert, key := formFile(r, "cert"), formFile(r, "key")
		if _, err := tls.X509KeyPair(cert, key); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(f.upCert), 0o700); err != nil {
			return err
		}
		if err := errors.Join(os.WriteFile(f.upCert, cert, 0o600), os.WriteFile(f.upKey, key, 0o600)); err != nil {
			return err
		}
		f.set(f.upCert, f.upKey)
		return nil
	})
	handle("POST /ssl/paths", true, func(w http.ResponseWriter, r *http.Request) error {
		var req struct{ Cert, Key string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return err
		}
		if _, err := tls.LoadX509KeyPair(req.Cert, req.Key); err != nil {
			return err
		}
		f.leave(req.Cert, req.Key)
		return nil
	})
	handle("POST /ssl/selfsigned", true, func(w http.ResponseWriter, r *http.Request) error {
		if _, err := os.Stat(f.selfCert); err != nil {
			if err := f.makeSelfSigned(); err != nil {
				return err
			}
		}
		f.leave(f.selfCert, f.selfKey)
		return nil
	})
	handle("POST /ssl/regenerate", true, func(w http.ResponseWriter, r *http.Request) error {
		if cert, _ := f.paths(); cert != f.selfCert {
			return errors.New("not using the self-signed certificate")
		}
		return f.makeSelfSigned()
	})
}

// leave switches to another pair and, like TorrServer, deletes an uploaded one.
func (f *fakeSSL) leave(cert, key string) {
	if current, _ := f.paths(); current == f.upCert && cert != f.upCert {
		_ = os.RemoveAll(filepath.Dir(f.upCert))
	}
	f.set(cert, key)
}

// status is GET /ssl/status's answer.
func (f *fakeSSL) status() map[string]any {
	certFile, keyFile := f.paths()
	info := map[string]any{"source": "none", "trusted": false}
	st := map[string]any{"enabled": f.enabled, "http_port": f.httpPort, "http_enabled": true, "cert": info}
	if !f.enabled {
		return st
	}
	port, _ := f.get()["SslPort"].(float64)
	if port == 0 {
		port = 8091
	}
	st["port"] = strconv.Itoa(int(port))
	if certFile == "" {
		return st
	}
	info["cert_file"], info["key_file"] = certFile, keyFile
	switch certFile {
	case f.selfCert:
		info["source"] = "self-signed"
	case f.upCert:
		info["source"] = "uploaded"
	default:
		info["source"] = "user"
	}
	raw, err := os.ReadFile(certFile) // #nosec G304 -- test fake: the configured certificate
	block, _ := pem.Decode(raw)
	if err != nil || block == nil {
		info["error"] = "cannot read the certificate"
		return st
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		info["error"] = err.Error()
		return st
	}
	info["subject"], info["issuer"] = leaf.Subject.String(), leaf.Issuer.String()
	info["dns_names"] = leaf.DNSNames
	info["not_before"], info["not_after"] = leaf.NotBefore, leaf.NotAfter
	return st
}

func refuse(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func formFile(r *http.Request, field string) []byte {
	f, _, err := r.FormFile(field)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	raw, _ := io.ReadAll(io.LimitReader(f, 1<<20))
	return raw
}
