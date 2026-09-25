package live_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/live"
)

// counter is a data source whose value the test controls and whose fetches it counts.
type counter struct {
	value   atomic.Int64
	fetches atomic.Int64
	fail    atomic.Bool
}

func (c *counter) fetch(context.Context) (any, error) {
	c.fetches.Add(1)
	if c.fail.Load() {
		return nil, errors.New("engine down")
	}
	return c.value.Load(), nil
}

func newHub(t *testing.T, sources map[string]*counter) *live.Hub {
	t.Helper()
	hub := live.New(func(topic string) (live.Poller, bool) {
		c, ok := sources[topic]
		if !ok {
			return live.Poller{}, false
		}
		return live.Poller{Interval: 10 * time.Millisecond, Fetch: c.fetch}, true
	}, live.WithGrace(50*time.Millisecond))
	t.Cleanup(hub.Close)
	return hub
}

// next waits for a change notification.
func next(t *testing.T, sub *live.Subscription) {
	t.Helper()
	select {
	case <-sub.C:
	case <-time.After(2 * time.Second):
		t.Fatal("no update")
	}
}

func quiet(t *testing.T, sub *live.Subscription, d time.Duration) {
	t.Helper()
	select {
	case <-sub.C:
		t.Fatal("notified although nothing changed")
	case <-time.After(d):
	}
}

func TestASubscriberGetsTheCurrentStateAndThenOnlyChanges(t *testing.T) {
	src := &counter{}
	src.value.Store(1)
	hub := newHub(t, map[string]*counter{"torrents": src})

	sub := hub.Subscribe("torrents")
	defer sub.Close()
	next(t, sub)
	if got := sub.Snapshot("torrents"); got.Value != int64(1) || got.Err != nil {
		t.Fatalf("first snapshot = %+v, want 1", got)
	}
	quiet(t, sub, 60*time.Millisecond) // polled several times, same value

	src.value.Store(2)
	next(t, sub)
	if got := sub.Snapshot("torrents").Value; got != int64(2) {
		t.Fatalf("snapshot after change = %v, want 2", got)
	}

	late := hub.Subscribe("torrents")
	defer late.Close()
	next(t, late) // a newcomer is told at once that there is state
	if got := late.Snapshot("torrents").Value; got != int64(2) {
		t.Fatalf("newcomer snapshot = %v, want 2", got)
	}
}

func TestOneFetchServesEverySubscriber(t *testing.T) {
	src := &counter{}
	hub := newHub(t, map[string]*counter{"torrents": src})
	subs := make([]*live.Subscription, 10)
	for i := range subs {
		subs[i] = hub.Subscribe("torrents")
		defer subs[i].Close()
	}
	time.Sleep(105 * time.Millisecond) // about ten intervals
	if got := src.fetches.Load(); got < 5 || got > 15 {
		t.Errorf("%d fetches for 10 subscribers over ~10 intervals, want about 10 (one per interval)", got)
	}
}

func TestPollingRunsOnlyWhileSomeoneWatches(t *testing.T) {
	src := &counter{}
	hub := newHub(t, map[string]*counter{"torrents": src})
	time.Sleep(40 * time.Millisecond)
	if got := src.fetches.Load(); got != 0 {
		t.Fatalf("%d fetches with no subscriber, want 0", got)
	}

	sub := hub.Subscribe("torrents")
	next(t, sub)
	sub.Close()
	time.Sleep(120 * time.Millisecond) // past the 50ms grace
	stopped := src.fetches.Load()
	time.Sleep(60 * time.Millisecond)
	if got := src.fetches.Load(); got != stopped {
		t.Errorf("still polling after the last subscriber left: %d → %d fetches", stopped, got)
	}
}

func TestAFailingSourceIsReportedAsState(t *testing.T) {
	src := &counter{}
	src.fail.Store(true)
	hub := newHub(t, map[string]*counter{"engine": src})
	sub := hub.Subscribe("engine")
	defer sub.Close()
	next(t, sub)
	if got := sub.Snapshot("engine"); got.Err == nil {
		t.Fatalf("snapshot of a failing source = %+v, want its error", got)
	}
	src.fail.Store(false)
	next(t, sub)
	if got := sub.Snapshot("engine"); got.Err != nil || got.Value != int64(0) {
		t.Fatalf("snapshot after recovery = %+v, want value 0", got)
	}
}

func TestOneSubscriptionFollowsSeveralTopics(t *testing.T) {
	engine, torrents := &counter{}, &counter{}
	hub := newHub(t, map[string]*counter{"engine": engine, "torrents": torrents})
	sub := hub.Subscribe("engine", "torrents", "unknown")
	defer sub.Close()
	next(t, sub)
	deadline := time.After(2 * time.Second)
	for sub.Snapshot("engine").Version == 0 || sub.Snapshot("torrents").Version == 0 {
		select {
		case <-sub.C:
		case <-deadline:
			t.Fatal("not every topic delivered a snapshot")
		}
	}
	if got := sub.Snapshot("unknown"); got.Version != 0 {
		t.Errorf("unknown topic has a snapshot: %+v", got)
	}
}

func TestTheHubCountsOpenSubscriptions(t *testing.T) {
	hub := newHub(t, map[string]*counter{"a": {}, "b": {}})
	one, two := hub.Subscribe("a", "b"), hub.Subscribe("a")
	if got := hub.Subscribers(); got != 2 {
		t.Errorf("open subscriptions = %d, want 2", got)
	}
	one.Close()
	one.Close() // closing twice counts once
	two.Close()
	if got := hub.Subscribers(); got != 0 {
		t.Errorf("after closing: %d, want 0", got)
	}
}
