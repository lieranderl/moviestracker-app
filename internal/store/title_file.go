package store

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

// TitleFile is a TitleStateStore in one owner-only JSON file, replaced
// atomically on every change.
type TitleFile struct {
	opts  options
	path  string
	mu    sync.Mutex
	users map[string]map[string]TitleState // by user, then by titleID
}

// OpenTitleFile reads the watch states kept at path; a missing file is none.
func OpenTitleFile(path string, opts ...Option) (*TitleFile, error) {
	f := &TitleFile{opts: newOptions(opts), path: path, users: map[string]map[string]TitleState{}}
	if err := readJSONFile(path, &f.users); err != nil {
		return nil, fmt.Errorf("read watch states %s: %w", path, err)
	}
	return f, nil
}

// SetTitleState implements TitleStateStore.
func (f *TitleFile) SetTitleState(_ context.Context, uid string, ts TitleState) error {
	id, err := checkTitleState(ts)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	titles := f.users[uid]
	if titles == nil {
		titles = map[string]TitleState{}
		f.users[uid] = titles
	}
	if ts.State == "" {
		delete(titles, id)
	} else {
		ts.UpdatedAt = f.opts.now().UTC()
		titles[id] = ts
	}
	if err := writeJSONFile(f.path, f.users); err != nil {
		return fmt.Errorf("save watch states: %w", err)
	}
	return nil
}

// TitleState implements TitleStateStore.
func (f *TitleFile) TitleState(_ context.Context, uid, kind string, tmdbID int) (TitleState, error) {
	id, err := titleID(kind, tmdbID)
	if err != nil {
		return TitleState{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.users[uid][id], nil
}

// TitleStates implements TitleStateStore.
func (f *TitleFile) TitleStates(_ context.Context, uid, state string) ([]TitleState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []TitleState{}
	for _, ts := range f.users[uid] {
		if ts.State == state {
			out = append(out, ts)
		}
	}
	slices.SortFunc(out, newestFirst)
	return out, nil
}
