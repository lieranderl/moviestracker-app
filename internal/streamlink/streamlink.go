// Package streamlink signs the links Moviestracker gives to external players
// (VLC, TVs, playlists), which cannot carry a sign-in cookie. A link grants
// one file of one torrent until it expires; changing the secret cancels all.
package streamlink

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrInvalid reports a token that was not signed by this secret or was changed.
	ErrInvalid = errors.New("streamlink: invalid link")
	// ErrExpired reports a genuine token past its lifetime.
	ErrExpired = errors.New("streamlink: link expired")
)

// Grant is what a valid token allows: streaming one file of one torrent.
type Grant struct {
	Hash string // lowercase BTIH
	File int    // TorrServer file index (from 1)
}

// Option configures a Signer.
type Option func(*Signer)

// WithClock overrides the time source for issuing and checking tokens.
func WithClock(now func() time.Time) Option {
	return func(s *Signer) {
		if now != nil {
			s.now = now
		}
	}
}

// Signer issues and checks tokens with one secret.
type Signer struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewSigner returns a signer whose tokens last ttl.
func NewSigner(secret []byte, ttl time.Duration, opts ...Option) *Signer {
	s := &Signer{secret: append([]byte(nil), secret...), ttl: ttl, now: time.Now}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Token returns a link token for file of the torrent hash:
// "<hash>.<file>.<expires unix>.<mac>", safe inside a URL path.
func (s *Signer) Token(hash string, file int) string {
	payload := strings.ToLower(hash) + "." + strconv.Itoa(file) + "." + strconv.FormatInt(s.now().Add(s.ttl).Unix(), 10)
	return payload + "." + s.mac(payload)
}

// Verify checks a token and returns what it grants.
func (s *Signer) Verify(token string) (Grant, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 4 || !isHash(parts[0]) {
		return Grant{}, ErrInvalid
	}
	payload := strings.Join(parts[:3], ".")
	if !hmac.Equal([]byte(parts[3]), []byte(s.mac(payload))) {
		return Grant{}, ErrInvalid
	}
	file, err := strconv.Atoi(parts[1])
	if err != nil || file < 1 {
		return Grant{}, ErrInvalid
	}
	expires, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return Grant{}, ErrInvalid
	}
	if s.now().Unix() >= expires {
		return Grant{}, ErrExpired
	}
	return Grant{Hash: parts[0], File: file}, nil
}

func (s *Signer) mac(payload string) string {
	m := hmac.New(sha256.New, s.secret)
	_, _ = m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func isHash(h string) bool {
	if len(h) != 40 {
		return false
	}
	for _, c := range h {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
