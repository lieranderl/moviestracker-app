package gateway

import (
	"context"
	"crypto/tls"
	"errors"
	"os"
	"sync"
	"time"
)

// Files names the certificate and key TorrServer serves HTTPS with
// (/ssl/status): the gateway's HTTPS port serves the same pair.
type Files func(context.Context) (cert, key string, err error)

// filesFor is how long the files TorrServer named are taken as current; a
// renewal of the same files is seen at once, by their modification time.
const filesFor = 30 * time.Second

// Certificates serves TorrServer's certificate on the gateway's HTTPS port,
// picking up renewals and new uploads without a restart.
type Certificates struct {
	files Files
	now   func() time.Time

	mu        sync.Mutex
	cert, key string    // the files TorrServer named
	askedAt   time.Time // when it named them
	loaded    *tls.Certificate
	modified  [2]time.Time // the files' modification times when loaded
}

// CertificatesOption configures Certificates.
type CertificatesOption func(*Certificates)

// WithClock sets the clock that ages the files TorrServer named (tests).
func WithClock(now func() time.Time) CertificatesOption {
	return func(c *Certificates) { c.now = now }
}

// NewCertificates serves the pair files names.
func NewCertificates(files Files, opts ...CertificatesOption) *Certificates {
	c := &Certificates{files: files, now: time.Now}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Get is a tls.Config GetCertificate: TorrServer's current pair, or an error
// (the handshake fails) while TorrServer serves no HTTPS.
func (c *Certificates) Get(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cert == "" || c.now().Sub(c.askedAt) >= filesFor {
		cert, key, err := c.files(hello.Context())
		if err != nil {
			return nil, err
		}
		if cert == "" || key == "" {
			return nil, errors.New("TorrServer names no certificate files")
		}
		c.cert, c.key, c.askedAt = cert, key, c.now()
	}
	modified, err := modTimes(c.cert, c.key)
	if err != nil {
		return nil, err
	}
	if c.loaded == nil || modified != c.modified {
		pair, err := tls.LoadX509KeyPair(c.cert, c.key)
		if err != nil {
			return nil, err
		}
		c.loaded, c.modified = &pair, modified
	}
	return c.loaded, nil
}

func modTimes(cert, key string) ([2]time.Time, error) {
	var out [2]time.Time
	for i, path := range []string{cert, key} {
		info, err := os.Stat(path)
		if err != nil {
			return out, err
		}
		out[i] = info.ModTime()
	}
	return out, nil
}
