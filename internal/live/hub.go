// Package live keeps the latest snapshot of each live topic (the torrent
// list, the engine, a playing torrent) and tells subscribers when it changes.
// Each topic is polled by one goroutine while anyone watches it, however many
// pages do: pages never poll the engine themselves.
package live

import (
	"context"
	"reflect"
	"sync"
	"time"
)

// Fetch reads a topic's current value.
type Fetch func(ctx context.Context) (any, error)

// Poller says how to keep a topic current.
type Poller struct {
	Interval time.Duration
	Fetch    Fetch
}

// Source resolves a topic name to its poller; false for an unknown topic.
type Source func(topic string) (Poller, bool)

// Snapshot is a topic's latest state. Version 0 means nothing fetched yet;
// Err is the last fetch's failure (then Value is nil).
type Snapshot struct {
	Value   any
	Err     error
	Version uint64
	At      time.Time
}

// Option configures a Hub.
type Option func(*Hub)

// WithGrace sets how long a topic keeps polling after its last subscriber
// leaves, so a page reload does not restart it (30s by default).
func WithGrace(d time.Duration) Option {
	return func(h *Hub) { h.grace = d }
}

// Hub owns the topics. It is safe for concurrent use.
type Hub struct {
	source Source
	grace  time.Duration
	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.Mutex
	topics map[string]*topic
	open   int // subscriptions not yet closed
}

type topic struct {
	name   string
	poller Poller
	snap   Snapshot
	subs   map[*Subscription]struct{}
	stop   context.CancelFunc // ends the polling goroutine
	idle   *time.Timer        // pending stop after the last subscriber left
}

// New returns a hub whose topics come from source.
func New(source Source, opts ...Option) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	h := &Hub{source: source, grace: 30 * time.Second, ctx: ctx, cancel: cancel, topics: map[string]*topic{}}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Close stops every poller.
func (h *Hub) Close() { h.cancel() }

// Subscription follows some topics. C receives a value whenever one of them
// has a new snapshot (at most one pending: several changes coalesce).
type Subscription struct {
	C      <-chan struct{}
	notify chan struct{}
	hub    *Hub
	topics []string
	once   sync.Once
}

// Subscribe follows topics, starting their pollers as needed. Unknown topics
// are ignored. If a topic already has state, C is signalled at once.
func (h *Hub) Subscribe(topics ...string) *Subscription {
	ch := make(chan struct{}, 1)
	sub := &Subscription{C: ch, notify: ch, hub: h}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.open++
	for _, name := range topics {
		t := h.topics[name]
		if t == nil {
			poller, ok := h.source(name)
			if !ok {
				continue
			}
			t = &topic{name: name, poller: poller, subs: map[*Subscription]struct{}{}}
			h.topics[name] = t
		}
		sub.topics = append(sub.topics, name)
		t.subs[sub] = struct{}{}
		if t.idle != nil {
			t.idle.Stop()
			t.idle = nil
		}
		if t.stop == nil {
			ctx, stop := context.WithCancel(h.ctx)
			t.stop = stop
			go h.poll(ctx, t)
		}
		if t.snap.Version > 0 {
			sub.signal()
		}
	}
	return sub
}

// Subscribers is how many subscriptions are open: one per live page stream.
func (h *Hub) Subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.open
}

// Snapshot returns the latest state of one of the subscription's topics.
func (s *Subscription) Snapshot(name string) Snapshot {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	if t := s.hub.topics[name]; t != nil {
		return t.snap
	}
	return Snapshot{}
}

// Close stops following; a topic nobody follows stops polling after the grace.
func (s *Subscription) Close() {
	s.once.Do(func() {
		h := s.hub
		h.mu.Lock()
		defer h.mu.Unlock()
		h.open--
		for _, name := range s.topics {
			t := h.topics[name]
			if t == nil {
				continue
			}
			delete(t.subs, s)
			if len(t.subs) == 0 && t.stop != nil && t.idle == nil {
				t.idle = time.AfterFunc(h.grace, func() { h.retire(t) })
			}
		}
	})
}

func (s *Subscription) signal() {
	select {
	case s.notify <- struct{}{}:
	default: // an update is already pending
	}
}

// retire stops a topic that is still unwatched after the grace period.
func (h *Hub) retire(t *topic) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(t.subs) > 0 || t.idle == nil {
		return
	}
	t.idle = nil
	t.stop()
	t.stop = nil
	delete(h.topics, t.name)
}

// poll fetches t now and then every interval until ctx ends.
func (h *Hub) poll(ctx context.Context, t *topic) {
	tick := time.NewTicker(t.poller.Interval)
	defer tick.Stop()
	for {
		fetchCtx, cancel := context.WithTimeout(ctx, max(t.poller.Interval, 2*time.Second))
		value, err := t.poller.Fetch(fetchCtx)
		cancel()
		if ctx.Err() != nil {
			return
		}
		h.publish(t, value, err)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// publish stores a fetch result and notifies subscribers when it differs
// from the previous one.
func (h *Hub) publish(t *topic, value any, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	prev := t.snap
	if prev.Version > 0 && sameError(prev.Err, err) && reflect.DeepEqual(prev.Value, value) {
		return
	}
	t.snap = Snapshot{Value: value, Err: err, Version: prev.Version + 1, At: time.Now()}
	for sub := range t.subs {
		sub.signal()
	}
}

func sameError(a, b error) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Error() == b.Error()
}
