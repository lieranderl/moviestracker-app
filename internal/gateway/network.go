package gateway

import (
	"net"
	"net/netip"
	"sync"
	"time"
)

// cgnat is 100.64.0.0/10, where Tailscale gives out addresses: a device on
// the owner's tailnet counts as home.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// clientAddr is the address of the device r comes from.
func clientAddr(remote string) (netip.Addr, bool) {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr.Unmap().WithZone(""), true
}

// atHome reports whether addr is on the home network: private, loopback or
// link-local addresses, or a tailnet.
func atHome(addr netip.Addr) bool {
	return addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || cgnat.Contains(addr)
}

// Failed logins are counted per device address: after maxFailures within
// failureWindow, it is refused until the window is over.
const (
	maxFailures   = 10
	failureWindow = time.Minute
	maxTracked    = 4096 // addresses remembered at once
)

type failures struct {
	start time.Time
	count int
}

// guessLimiter counts failed logins per address.
type guessLimiter struct {
	mu    sync.Mutex
	addrs map[netip.Addr]failures
}

func newGuessLimiter() *guessLimiter { return &guessLimiter{addrs: map[netip.Addr]failures{}} }

// blocked reports whether addr has failed too often lately.
func (l *guessLimiter) blocked(addr netip.Addr, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, ok := l.addrs[addr]
	return ok && now.Sub(f.start) < failureWindow && f.count >= maxFailures
}

// failed counts a failed login from addr.
func (l *guessLimiter) failed(addr netip.Addr, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	f, ok := l.addrs[addr]
	if !ok || now.Sub(f.start) >= failureWindow {
		if !ok && len(l.addrs) >= maxTracked {
			for a, old := range l.addrs {
				if now.Sub(old.start) >= failureWindow {
					delete(l.addrs, a)
				}
			}
			if len(l.addrs) >= maxTracked {
				return
			}
		}
		f = failures{start: now}
	}
	f.count++
	l.addrs[addr] = f
}
