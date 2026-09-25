// Package streams follows what is being played through Moviestracker's
// stream proxy: one session per device and torrent, from the requests the
// proxy sees, so the dashboard knows what plays without asking TorrServer.
package streams

import (
	"slices"
	"sync"
	"time"
)

// Kind is how a session streams.
type Kind string

// Stream kinds.
const (
	Direct Kind = "direct" // the file itself, with byte ranges
	HLS    Kind = "hls"    // GStreamer HLS playlists and segments
)

// Request is one proxied request of a player.
type Request struct {
	Client string // the device (IP address)
	Viewer string // who: an account name, or "Shared link"
	Hash   string
	File   int // TorrServer file index; 0 when the request does not say (HLS segments)
	Audio  int // HLS audio track, when the request says
	Kind   Kind
	Offset int64 // direct: first byte requested
	// Segment is the HLS segment requested; -1 for playlists and init data.
	Segment int
	Bytes   int64
}

// Session is one device playing one torrent.
type Session struct {
	Client, Viewer, Hash string
	File, Audio          int
	Kind                 Kind
	Started, LastSeen    time.Time
	Ended                time.Time // when it went quiet; zero while it plays
	Offset               int64     // last byte range start (direct)
	Segment              int       // last segment requested (HLS), -1 before the first
	Bytes                int64     // sent to the device so far
	Rate                 float64   // bytes per second over the last rateWindow; set by Active
	open                 int       // responses still being sent
	sent                 []mark    // recent byte counts, for Rate
}

// mark is the bytes sent by a moment.
type mark struct {
	at    time.Time
	bytes int64
}

// rateWindow is how far back a session's delivery speed looks; markEvery is
// how often Sent notes the bytes so far.
const (
	rateWindow = 10 * time.Second
	markEvery  = 500 * time.Millisecond
)

// rate is the speed over the last rateWindow: zero when nothing was sent in it.
func (s *Session) rate(now time.Time) float64 {
	start := now.Add(-rateWindow)
	if len(s.sent) == 0 || s.sent[len(s.sent)-1].at.Before(start) {
		return 0
	}
	base := s.sent[0]
	for _, m := range s.sent {
		if m.at.After(start) {
			break
		}
		base = m
	}
	secs := now.Sub(base.at).Seconds()
	if secs <= 0 {
		return 0
	}
	return float64(s.Bytes-base.bytes) / secs
}

// Progress is how far into a file of size bytes a direct stream reads, in %.
func (s Session) Progress(size int64) float64 {
	if size <= 0 {
		return 0
	}
	return min(100, float64(s.Offset)*100/float64(size))
}

// Position is where an HLS stream plays, given the segment length.
func (s Session) Position(segment time.Duration) time.Duration {
	if s.Segment < 0 {
		return 0
	}
	return time.Duration(s.Segment) * segment
}

// Option configures a Tracker.
type Option func(*Tracker)

// WithClock overrides the time source.
func WithClock(now func() time.Time) Option {
	return func(t *Tracker) { t.now = now }
}

// Tracker holds the sessions. It is safe for concurrent use.
type Tracker struct {
	idle time.Duration
	now  func() time.Time

	mu       sync.Mutex
	sessions map[key]*Session
	finished []Session // oldest first
}

// Finished sessions are kept for a day, and at most this many.
const (
	keepFinished = 24 * time.Hour
	maxFinished  = 200
)

type key struct{ client, hash string }

// New returns a tracker whose sessions end idle after the last request.
func New(idle time.Duration, opts ...Option) *Tracker {
	t := &Tracker{idle: idle, now: time.Now, sessions: map[key]*Session{}}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Flow is one response being sent to a player.
type Flow struct {
	t *Tracker
	s *Session
}

// Sent counts bytes as they reach the player, so a long response (VLC
// reading a whole file) shows its progress and speed while it plays.
func (f Flow) Sent(n int64) {
	f.t.mu.Lock()
	defer f.t.mu.Unlock()
	now := f.t.now()
	f.s.Bytes += n
	if last := len(f.s.sent) - 1; last >= 0 && now.Sub(f.s.sent[last].at) < markEvery {
		return // chunks arrive fast; a mark every half second is enough
	}
	f.s.sent = append(f.s.sent, mark{at: now, bytes: f.s.Bytes})
	cut := 0
	for cut < len(f.s.sent)-1 && now.Sub(f.s.sent[cut+1].at) > rateWindow {
		cut++
	}
	f.s.sent = f.s.sent[cut:]
}

// Done ends the response.
func (f Flow) Done() {
	f.t.mu.Lock()
	defer f.t.mu.Unlock()
	f.s.open--
	f.s.LastSeen = f.t.now()
}

// Open records a request whose response is starting. A session with a
// response still being sent never goes idle.
func (t *Tracker) Open(r Request) Flow {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.touch(r)
	s.open++
	if len(s.sent) == 0 {
		s.sent = append(s.sent, mark{at: t.now(), bytes: s.Bytes})
	}
	return Flow{t: t, s: s}
}

// Observe records a request that has already been answered.
func (t *Tracker) Observe(r Request) {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := t.touch(r)
	s.Bytes += r.Bytes
}

// touch creates or updates the session of r; the caller holds t.mu.
func (t *Tracker) touch(r Request) *Session {
	now := t.now()
	k := key{r.Client, r.Hash}
	s := t.sessions[k]
	if s != nil && t.expired(s, now) {
		t.finish(s)
	}
	if s == nil || t.expired(s, now) {
		s = &Session{Client: r.Client, Hash: r.Hash, Kind: r.Kind, Started: now, Segment: -1}
		t.sessions[k] = s
	}
	s.LastSeen = now
	s.Kind = r.Kind
	if r.Viewer != "" {
		s.Viewer = r.Viewer
	}
	if r.File > 0 {
		s.File = r.File
	}
	if r.Kind == HLS {
		if r.Segment >= 0 {
			s.Segment = r.Segment
		}
		if r.File > 0 {
			s.Audio = r.Audio
		}
	} else {
		s.Offset = r.Offset
	}
	return s
}

func (t *Tracker) expired(s *Session, now time.Time) bool {
	return s.open == 0 && now.Sub(s.LastSeen) > t.idle
}

// finish moves a session that went quiet to the finished list; the caller
// holds t.mu.
func (t *Tracker) finish(s *Session) {
	done := *s
	done.Ended, done.sent = s.LastSeen, nil
	t.finished = append(t.finished, done)
	if len(t.finished) > maxFinished {
		t.finished = t.finished[len(t.finished)-maxFinished:]
	}
}

// prune finishes the sessions that went quiet; the caller holds t.mu.
func (t *Tracker) prune(now time.Time) {
	for k, s := range t.sessions {
		if t.expired(s, now) {
			t.finish(s)
			delete(t.sessions, k)
		}
	}
	slices.SortFunc(t.finished, func(a, b Session) int { return a.Ended.Compare(b.Ended) })
	cut := 0
	for cut < len(t.finished) && now.Sub(t.finished[cut].Ended) > keepFinished {
		cut++
	}
	t.finished = t.finished[cut:]
}

// Finished returns the sessions that ended in the last day, newest first.
func (t *Tracker) Finished() []Session {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prune(t.now())
	out := slices.Clone(t.finished)
	slices.Reverse(out)
	return out
}

// Active returns the sessions still playing, oldest first.
func (t *Tracker) Active() []Session {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.prune(t.now())
	now := t.now()
	out := make([]Session, 0, len(t.sessions))
	for _, s := range t.sessions {
		c := *s
		c.Rate = s.rate(now)
		c.sent = nil
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b Session) int { return a.Started.Compare(b.Started) })
	return out
}
