// Package events keeps the latest warnings and errors Moviestracker logs, so
// the dashboard can show recent problems without anyone reading the log.
package events

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"time"
)

// Event is one logged problem.
type Event struct {
	Time    time.Time
	Level   slog.Level
	Message string
	Detail  string // the "error" attribute, when the record has one
}

// Recorder is a slog.Handler that passes every record on and remembers the
// latest warnings and errors. It is safe for concurrent use.
type Recorder struct {
	next slog.Handler
	ring *ring
}

type ring struct {
	mu     sync.Mutex
	size   int
	events []Event // oldest first
}

// NewRecorder wraps next and keeps the last size problems.
func NewRecorder(next slog.Handler, size int) *Recorder {
	return &Recorder{next: next, ring: &ring{size: size}}
}

// Enabled reports whether next handles level.
func (r *Recorder) Enabled(ctx context.Context, level slog.Level) bool {
	return r.next.Enabled(ctx, level)
}

// Handle remembers warnings and errors, then passes the record on.
func (r *Recorder) Handle(ctx context.Context, rec slog.Record) error {
	if rec.Level >= slog.LevelWarn {
		e := Event{Time: rec.Time, Level: rec.Level, Message: rec.Message}
		rec.Attrs(func(a slog.Attr) bool {
			if a.Key == "error" {
				e.Detail = a.Value.String()
				return false
			}
			return true
		})
		r.ring.add(e)
	}
	return r.next.Handle(ctx, rec)
}

// WithAttrs keeps recording into the same list.
func (r *Recorder) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Recorder{next: r.next.WithAttrs(attrs), ring: r.ring}
}

// WithGroup keeps recording into the same list.
func (r *Recorder) WithGroup(name string) slog.Handler {
	return &Recorder{next: r.next.WithGroup(name), ring: r.ring}
}

// Recent returns the remembered problems, newest first.
func (r *Recorder) Recent() []Event {
	r.ring.mu.Lock()
	defer r.ring.mu.Unlock()
	out := slices.Clone(r.ring.events)
	slices.Reverse(out)
	return out
}

func (g *ring) add(e Event) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.events = append(g.events, e)
	if len(g.events) > g.size {
		g.events = g.events[len(g.events)-g.size:]
	}
}
