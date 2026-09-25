package handlers

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

type loginWindow struct {
	startedAt time.Time
	attempts  int
}

type loginLimiter struct {
	mu         sync.Mutex
	clients    map[string]loginWindow
	maxClients int
	attempts   int
	window     time.Duration
}

func newLoginLimiter(maxClients, attempts int, window time.Duration) *loginLimiter {
	return &loginLimiter{
		clients:    make(map[string]loginWindow),
		maxClients: maxClients,
		attempts:   attempts,
		window:     window,
	}
}

func (l *loginLimiter) Allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, exists := l.clients[key]
	if exists && now.Sub(entry.startedAt) >= l.window {
		delete(l.clients, key)
		exists = false
	}

	if !exists && len(l.clients) >= l.maxClients {
		l.pruneExpiredLocked(now)
		if len(l.clients) >= l.maxClients {
			return false
		}
	}

	if !exists {
		l.clients[key] = loginWindow{startedAt: now, attempts: 1}
		return true
	}
	if entry.attempts >= l.attempts {
		return false
	}
	entry.attempts++
	l.clients[key] = entry
	return true
}

func (l *loginLimiter) pruneExpiredLocked(now time.Time) {
	for key, entry := range l.clients {
		if now.Sub(entry.startedAt) >= l.window {
			delete(l.clients, key)
		}
	}
}

func parseTrustedProxyCIDRs(values []string) ([]netip.Prefix, error) {
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", value, err)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func clientIP(r *http.Request, trusted []netip.Prefix) string {
	peer, ok := remoteIP(r.RemoteAddr)
	if !ok {
		if r.RemoteAddr != "" {
			return r.RemoteAddr
		}
		return "unknown"
	}
	if !containsIP(trusted, peer) {
		return peer.String()
	}

	forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if forwarded == "" {
		return peer.String()
	}
	parts := strings.Split(forwarded, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		candidate, err := netip.ParseAddr(strings.TrimSpace(parts[i]))
		if err != nil {
			return peer.String()
		}
		candidate = candidate.Unmap()
		if !containsIP(trusted, candidate) {
			return candidate.String()
		}
	}
	return peer.String()
}

func remoteIP(remoteAddr string) (netip.Addr, bool) {
	if addrPort, err := netip.ParseAddrPort(remoteAddr); err == nil {
		return addrPort.Addr().Unmap(), true
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap(), true
}

func containsIP(prefixes []netip.Prefix, addr netip.Addr) bool {
	for _, prefix := range prefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}
