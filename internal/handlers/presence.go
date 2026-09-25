package handlers

import (
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// onlineFor is how long after their last request someone counts as online;
// an open page with a live stream keeps them online however long it stays.
const onlineFor = 5 * time.Minute

// presence remembers who uses Moviestracker from which device. It is safe
// for concurrent use.
type presence struct {
	mu      sync.Mutex
	devices map[deviceKey]*device
}

type deviceKey struct{ user, ip, name string }

type device struct {
	user, name, ip string // account, "Safari on iPhone", address
	lastSeen       time.Time
	open           int // requests still running, such as a page's live stream
}

// onlineDevice is a device in use now.
type onlineDevice struct {
	User, Name, IP string
	LastSeen       time.Time
	Live           bool // a page with a live stream is open
}

func newPresence() *presence {
	return &presence{devices: map[deviceKey]*device{}}
}

// track records a signed-in request for its whole length.
func (p *presence) track(user, ip, userAgent string, now time.Time) (done func()) {
	k := deviceKey{user, ip, deviceName(userAgent)}
	p.mu.Lock()
	defer p.mu.Unlock()
	d := p.devices[k]
	if d == nil {
		d = &device{user: k.user, name: k.name, ip: k.ip}
		p.devices[k] = d
	}
	d.lastSeen = now
	d.open++
	return func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		d.open--
		d.lastSeen = time.Now()
	}
}

// online lists the devices in use now, by person, most recent first.
func (p *presence) online(now time.Time) []onlineDevice {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []onlineDevice
	for k, d := range p.devices {
		live := d.open > 0
		if !live && now.Sub(d.lastSeen) > onlineFor {
			if now.Sub(d.lastSeen) > 24*time.Hour {
				delete(p.devices, k)
			}
			continue
		}
		out = append(out, onlineDevice{User: d.user, Name: d.name, IP: d.ip, LastSeen: d.lastSeen, Live: live})
	}
	slices.SortFunc(out, func(a, b onlineDevice) int {
		if c := strings.Compare(a.User, b.User); c != 0 {
			return c
		}
		return b.LastSeen.Compare(a.LastSeen)
	})
	return out
}

// presenceMiddleware records who is using Moviestracker, and from where.
func (s *Server) presenceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user := s.userFromRequest(r); user != nil {
			done := s.presence.track(user.Username, clientIP(r, s.trustedProxies), r.UserAgent(), time.Now())
			defer done()
		}
		next.ServeHTTP(w, r)
	})
}

// deviceName names a browser or player from its User-Agent, for people:
// "Safari on iPhone", "Chrome on Windows", "VLC".
func deviceName(ua string) string {
	has := func(s string) bool { return strings.Contains(ua, s) }
	for _, player := range []string{"VLC", "IINA", "Infuse", "Kodi", "mpv"} {
		if has(player) {
			return player
		}
	}
	system := ""
	switch {
	case has("iPhone"):
		system = "iPhone"
	case has("iPad"):
		system = "iPad"
	case has("Android"):
		system = "Android"
	case has("Macintosh"), has("Mac OS X"):
		system = "macOS"
	case has("Windows"):
		system = "Windows"
	case has("CrOS"):
		system = "ChromeOS"
	case has("Linux"):
		system = "Linux"
	}
	browser := ""
	switch {
	case has("Edg/"):
		browser = "Edge"
	case has("Firefox/"), has("FxiOS/"):
		browser = "Firefox"
	case has("Chrome/"), has("CriOS/"):
		browser = "Chrome"
	case has("Safari/"):
		browser = "Safari"
	}
	switch {
	case browser != "" && system != "":
		return browser + " on " + system
	case browser != "":
		return browser
	case system != "":
		return system
	}
	return "A browser"
}
