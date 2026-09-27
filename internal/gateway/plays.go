package gateway

import (
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/streams"
)

// signedInFor is how long a device's streams count as the app's that last
// signed in from it: a TV app lists torrents, then its video player plays
// the link without a login.
const signedInFor = 10 * time.Minute

// seenApp is the app that last signed in from a device, and when.
type seenApp struct {
	name string
	at   time.Time
}

// signedInFrom notes that the app called name signed in from addr.
func (g *Gateway) signedInFrom(addr netip.Addr, name string, now time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for a, s := range g.seen {
		if now.Sub(s.at) > signedInFor {
			delete(g.seen, a)
		}
	}
	g.seen[addr] = seenApp{name: name, at: now}
}

// viewer names who plays from addr: the app whose login the request
// carries, else the app that signed in from that device lately; "" when
// none did (the dashboard says "Other app").
func (g *Gateway) viewer(addr netip.Addr, login string, now time.Time) string {
	if login != "" {
		return login
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if s, ok := g.seen[addr]; ok && now.Sub(s.at) <= signedInFor {
		return s.name
	}
	return ""
}

// playOf describes r for the dashboard when it plays a file: TorrServer's
// /stream?link=…&index=…&play, /play/{hash}/{file}, or GStreamer HLS
// under /gst/{hash}/.
func playOf(r *http.Request) (streams.Request, bool) {
	if r.Method != http.MethodGet {
		return streams.Request{}, false
	}
	play := streams.Request{Kind: streams.Direct, Offset: streams.RangeStart(r.Header.Get("Range")), Segment: -1, OtherApp: true}
	p, q := r.URL.Path, r.URL.Query()
	switch {
	case p == "/stream" || strings.HasPrefix(p, "/stream/"):
		if !q.Has("play") {
			return streams.Request{}, false // links, playlists, stats
		}
		hash, ok := streamedHash(r)
		file, err := strconv.Atoi(q.Get("index"))
		if !ok || err != nil || file < 1 {
			return streams.Request{}, false
		}
		play.Hash, play.File = hash, file
	case strings.HasPrefix(p, "/play/"):
		hash, ok := streamedHash(r)
		file, err := strconv.Atoi(p[strings.LastIndex(p, "/")+1:])
		if !ok || err != nil || file < 1 {
			return streams.Request{}, false
		}
		play.Hash, play.File = hash, file
	case strings.HasPrefix(p, "/gst/"):
		hash, rest, _ := strings.Cut(strings.TrimPrefix(p, "/gst/"), "/")
		hash, ok := infoHash(hash)
		if !ok {
			return streams.Request{}, false
		}
		play.Hash, play.Kind = hash, streams.HLS
		play.File, play.Audio, play.Segment = streams.HLSPosition(rest, q)
	default:
		return streams.Request{}, false
	}
	return play, true
}

// counted passes a stream to the player, counting what it sends for the
// dashboard once TorrServer answers with the stream.
type counted struct {
	http.ResponseWriter
	plays   *streams.Tracker
	play    streams.Request
	flow    *streams.Flow
	written bool
}

func (c *counted) WriteHeader(code int) {
	if !c.written {
		c.written = true
		if code >= 200 && code < 300 {
			f := c.plays.Open(c.play)
			c.flow = &f
		}
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *counted) Write(b []byte) (int, error) {
	if !c.written {
		c.WriteHeader(http.StatusOK)
	}
	n, err := c.ResponseWriter.Write(b)
	if c.flow != nil && n > 0 {
		c.flow.Sent(int64(n))
	}
	return n, err
}

// Unwrap lets the proxy flush the stream as it goes.
func (c *counted) Unwrap() http.ResponseWriter { return c.ResponseWriter }

func (c *counted) done() {
	if c.flow != nil {
		c.flow.Done()
	}
}
