package tmdb

import (
	"context"
	"sync"
	"time"
)

// defaultDetailCacheSize bounds the detail/search cache so a crawl over many
// ids cannot grow memory without limit.
const defaultDetailCacheSize = 512

type detailEntry struct {
	value   any
	expires time.Time
}

type detailCall struct {
	done  chan struct{}
	value any
	err   error
}

// detailCache is a bounded TTL cache whose concurrent misses for the same key
// share one upstream fetch.
type detailCache struct {
	mu       sync.Mutex
	entries  map[string]detailEntry
	inflight map[string]*detailCall
	max      int
}

func newDetailCache(max int) *detailCache {
	return &detailCache{
		entries:  make(map[string]detailEntry),
		inflight: make(map[string]*detailCall),
		max:      max,
	}
}

// cached returns the fresh cached value for key or fetches it once. Cached
// values are shared between callers and must be treated as read-only.
func cached[T any](ctx context.Context, c *Client, key string, fetch func(context.Context) (T, error)) (T, error) {
	var zero T
	dc := c.details
	dc.mu.Lock()
	if e, ok := dc.entries[key]; ok && c.now().Before(e.expires) {
		dc.mu.Unlock()
		return e.value.(T), nil
	}
	call, joined := dc.inflight[key]
	if !joined {
		call = &detailCall{done: make(chan struct{})}
		dc.inflight[key] = call
	}
	dc.mu.Unlock()

	if !joined {
		// Detach from the leader's cancellation so followers are not failed by
		// one impatient client; the HTTP client timeout still bounds the fetch.
		value, err := fetch(context.WithoutCancel(ctx))
		call.value, call.err = value, err
		dc.mu.Lock()
		delete(dc.inflight, key)
		if err == nil {
			dc.storeLocked(key, value, c.now().Add(c.cacheTTL), c.now())
		}
		dc.mu.Unlock()
		close(call.done)
	}

	select {
	case <-call.done:
	case <-ctx.Done():
		return zero, ctx.Err()
	}
	if call.err != nil {
		return zero, call.err
	}
	return call.value.(T), nil
}

func (dc *detailCache) storeLocked(key string, value any, expires, now time.Time) {
	if len(dc.entries) >= dc.max {
		for k, e := range dc.entries {
			if !now.Before(e.expires) {
				delete(dc.entries, k)
			}
		}
	}
	if len(dc.entries) >= dc.max {
		for k := range dc.entries {
			delete(dc.entries, k)
			break
		}
	}
	dc.entries[key] = detailEntry{value: value, expires: expires}
}
