package store

import (
	"context"
	"fmt"
	"sync"
)

// ReviewFile is a ReviewStore in one owner-only JSON file, replaced
// atomically on every change.
type ReviewFile struct {
	opts  options
	path  string
	mu    sync.Mutex
	users map[string]map[string]Review // by user, then by titleID
}

// OpenReviewFile reads the ratings and notes kept at path; a missing file
// is none.
func OpenReviewFile(path string, opts ...Option) (*ReviewFile, error) {
	f := &ReviewFile{opts: newOptions(opts), path: path, users: map[string]map[string]Review{}}
	if err := readJSONFile(path, &f.users); err != nil {
		return nil, fmt.Errorf("read reviews %s: %w", path, err)
	}
	return f, nil
}

// SaveReview implements ReviewStore. A save that fails leaves the review
// as it was, so trying again saves it anew.
func (f *ReviewFile) SaveReview(_ context.Context, uid string, r Review) error {
	r, id, empty, err := checkReview(r)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	reviews := f.users[uid]
	if reviews == nil {
		reviews = map[string]Review{}
		f.users[uid] = reviews
	}
	before, had := reviews[id]
	if empty {
		delete(reviews, id)
	} else {
		r.UpdatedAt = f.opts.now().UTC()
		reviews[id] = r
	}
	if err := writeJSONFile(f.path, f.users); err != nil {
		if had {
			reviews[id] = before
		} else {
			delete(reviews, id)
		}
		return fmt.Errorf("save reviews: %w", err)
	}
	return nil
}

// Review implements ReviewStore.
func (f *ReviewFile) Review(_ context.Context, uid, kind string, tmdbID int) (Review, error) {
	id, err := titleID(kind, tmdbID)
	if err != nil {
		return Review{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.users[uid][id], nil
}
