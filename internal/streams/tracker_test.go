package streams_test

import (
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/streams"
)

const hash = "dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c"

type clock struct{ now time.Time }

func (c *clock) Now() time.Time          { return c.now }
func (c *clock) advance(d time.Duration) { c.now = c.now.Add(d) }
func newTracker(c *clock) *streams.Tracker {
	return streams.New(30*time.Second, streams.WithClock(c.Now))
}

func TestRangeRequestsFromOnePlayerAreOneSessionWithItsPosition(t *testing.T) {
	c := &clock{now: time.Date(2026, 9, 25, 20, 0, 0, 0, time.UTC)}
	tr := newTracker(c)
	tr.Observe(streams.Request{Client: "192.168.1.20", Viewer: "Shared link", Hash: hash, File: 2, Kind: streams.Direct, Offset: 0})
	c.advance(10 * time.Second)
	tr.Observe(streams.Request{Client: "192.168.1.20", Viewer: "Shared link", Hash: hash, File: 2, Kind: streams.Direct, Offset: 750_000_000, Bytes: 4 << 20})

	active := tr.Active()
	if len(active) != 1 {
		t.Fatalf("%d sessions, want 1: %+v", len(active), active)
	}
	s := active[0]
	if s.Hash != hash || s.File != 2 || s.Kind != streams.Direct || s.Offset != 750_000_000 || s.Bytes != 4<<20 {
		t.Errorf("session = %+v", s)
	}
	if got := s.Progress(1_000_000_000); got != 75 {
		t.Errorf("Progress() = %v%%, want 75%%", got)
	}
	if !s.Started.Equal(c.now.Add(-10*time.Second)) || !s.LastSeen.Equal(c.now) {
		t.Errorf("started %v, last seen %v", s.Started, s.LastSeen)
	}
}

func TestHLSSegmentsAreOneSessionWithAPositionInSeconds(t *testing.T) {
	c := &clock{now: time.Date(2026, 9, 25, 20, 0, 0, 0, time.UTC)}
	tr := newTracker(c)
	tr.Observe(streams.Request{Client: "10.0.0.5", Viewer: "Alex", Hash: hash, File: 1, Kind: streams.HLS, Audio: 1, Segment: -1}) // master playlist
	for seg := range 5 {
		c.advance(6 * time.Second)
		tr.Observe(streams.Request{Client: "10.0.0.5", Viewer: "Alex", Hash: hash, Kind: streams.HLS, Segment: seg})
	}
	active := tr.Active()
	if len(active) != 1 {
		t.Fatalf("%d sessions, want 1", len(active))
	}
	s := active[0]
	if s.File != 1 || s.Audio != 1 || s.Segment != 4 || s.Viewer != "Alex" {
		t.Errorf("session = %+v, want file 1, audio 1, last segment 4", s)
	}
	if got := s.Position(6 * time.Second); got != 24*time.Second {
		t.Errorf("Position() = %v, want 24s", got)
	}
}

func TestEachDeviceIsItsOwnSession(t *testing.T) {
	c := &clock{now: time.Date(2026, 9, 25, 20, 0, 0, 0, time.UTC)}
	tr := newTracker(c)
	tr.Observe(streams.Request{Client: "192.168.1.20", Hash: hash, File: 1, Kind: streams.Direct})
	tr.Observe(streams.Request{Client: "192.168.1.31", Hash: hash, File: 1, Kind: streams.Direct})
	if n := len(tr.Active()); n != 2 {
		t.Errorf("%d sessions for two devices, want 2", n)
	}
}

func TestASessionEndsAfterItsPlayerGoesQuiet(t *testing.T) {
	c := &clock{now: time.Date(2026, 9, 25, 20, 0, 0, 0, time.UTC)}
	tr := newTracker(c)
	tr.Observe(streams.Request{Client: "192.168.1.20", Hash: hash, File: 1, Kind: streams.Direct})
	c.advance(29 * time.Second)
	if n := len(tr.Active()); n != 1 {
		t.Fatalf("session gone after 29s: %d", n)
	}
	c.advance(2 * time.Second)
	if n := len(tr.Active()); n != 0 {
		t.Errorf("%d sessions 31s after the last request, want 0", n)
	}
}

func TestAStreamStillBeingSentIsNotIdle(t *testing.T) {
	c := &clock{now: time.Date(2026, 9, 25, 20, 0, 0, 0, time.UTC)}
	tr := newTracker(c)
	flow := tr.Open(streams.Request{Client: "192.168.1.20", Hash: hash, File: 1, Kind: streams.Direct})
	c.advance(10 * time.Minute) // one long response: VLC reading a whole file
	if n := len(tr.Active()); n != 1 {
		t.Fatalf("a response still streaming was dropped as idle")
	}
	flow.Sent(512 << 20)
	flow.Done()
	if s := tr.Active()[0]; s.Bytes != 512<<20 {
		t.Errorf("bytes after the response = %d, want 512 MB", s.Bytes)
	}
	c.advance(31 * time.Second)
	if n := len(tr.Active()); n != 0 {
		t.Errorf("session still active 31s after its response ended")
	}
}

func TestFinishedSessionsAreRememberedForADayNewestFirst(t *testing.T) {
	c := &clock{now: time.Date(2026, 9, 25, 20, 0, 0, 0, time.UTC)}
	tr := newTracker(c)
	// Anna's player reads the file in one long response for 20 minutes.
	flow := tr.Open(streams.Request{Client: "192.168.1.20", Viewer: "anna", Hash: hash, File: 1, Kind: streams.Direct})
	c.advance(20 * time.Minute)
	flow.Sent(1000)
	flow.Done()
	c.advance(time.Second)
	tr.Observe(streams.Request{Client: "192.168.1.31", Viewer: "Shared link", Hash: hash, File: 2, Kind: streams.HLS, Segment: 3})
	if len(tr.Finished()) != 0 {
		t.Fatal("sessions still playing are listed as finished")
	}

	c.advance(time.Minute) // both have gone quiet
	played := tr.Finished()
	if len(played) != 2 || played[0].Viewer != "Shared link" || played[1].Viewer != "anna" {
		t.Fatalf("finished = %+v, want the TV's session, then anna's", played)
	}
	anna := played[1]
	if anna.Bytes != 1000 || anna.Ended.Sub(anna.Started) != 20*time.Minute {
		t.Errorf("anna's session = %+v, want 1000 bytes over 20 minutes (to its last request)", anna)
	}

	c.advance(25 * time.Hour)
	if n := len(tr.Finished()); n != 0 {
		t.Errorf("%d sessions older than a day are still listed", n)
	}
}

func TestALongResponseCountsItsBytesAndSpeedWhileItPlays(t *testing.T) {
	c := &clock{now: time.Date(2026, 9, 25, 20, 0, 0, 0, time.UTC)}
	tr := newTracker(c)
	flow := tr.Open(streams.Request{Client: "192.168.1.20", Hash: hash, File: 1, Kind: streams.Direct})
	for range 4 { // VLC reads 2 MB a second for 20 seconds
		c.advance(5 * time.Second)
		flow.Sent(10 << 20)
	}
	s := tr.Active()[0]
	if s.Bytes != 40<<20 {
		t.Errorf("bytes while playing = %d, want 40 MB counted as they are sent", s.Bytes)
	}
	if s.Rate < 1.9*(1<<20) || s.Rate > 2.1*(1<<20) {
		t.Errorf("rate = %.0f B/s, want about 2 MB/s", s.Rate)
	}
	c.advance(30 * time.Second) // stalled: nothing sent
	if s := tr.Active()[0]; s.Rate != 0 {
		t.Errorf("rate after 30 quiet seconds = %.0f, want 0", s.Rate)
	}
}
