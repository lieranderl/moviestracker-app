package store

import (
	"context"
	"fmt"
	"maps"
	"sync"
)

// ListFile is a CustomListStore in one owner-only JSON file, replaced
// atomically on every change.
type ListFile struct {
	opts  options
	path  string
	mu    sync.Mutex
	users map[string]shelf
}

// OpenListFile reads the lists kept at path; a missing file is none.
func OpenListFile(path string, opts ...Option) (*ListFile, error) {
	f := &ListFile{opts: newOptions(opts), path: path, users: map[string]shelf{}}
	if err := readJSONFile(path, &f.users); err != nil {
		return nil, fmt.Errorf("read lists %s: %w", path, err)
	}
	return f, nil
}

// shelf is uid's lists, made empty on first use. f.mu must be held.
func (f *ListFile) shelf(uid string) shelf {
	s := f.users[uid]
	if s == nil {
		s = shelf{}
		f.users[uid] = s
	}
	return s
}

// change applies change to uid's lists and saves them when it says it
// changed them. A save that fails puts them back as they were, so trying
// again changes and saves them anew. f.mu must be held.
func (f *ListFile) change(uid string, change func(shelf) (bool, error)) error {
	before := maps.Clone(f.users[uid]) // changes replace a list's titles, never edit them
	changed, err := change(f.shelf(uid))
	if err != nil || !changed {
		return err
	}
	if err := writeJSONFile(f.path, f.users); err != nil {
		f.users[uid] = before
		return fmt.Errorf("save lists: %w", err)
	}
	return nil
}

// CreateCustomList implements CustomListStore.
func (f *ListFile) CreateCustomList(_ context.Context, uid, name string) (CustomList, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var l CustomList
	err := f.change(uid, func(s shelf) (bool, error) {
		var err error
		l, err = s.create(name, f.opts.now().UTC())
		return err == nil, err
	})
	if err != nil {
		return CustomList{}, err
	}
	return l, nil
}

// CustomLists implements CustomListStore.
func (f *ListFile) CustomLists(_ context.Context, uid string) ([]CustomList, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.users[uid].all(), nil
}

// RemoveCustomList implements CustomListStore.
func (f *ListFile) RemoveCustomList(_ context.Context, uid, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.change(uid, func(s shelf) (bool, error) { return true, s.delete(id) })
}

// CustomList implements CustomListStore.
func (f *ListFile) CustomList(_ context.Context, uid, id string) (CustomList, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, ok := f.users[uid].get(id)
	return l, ok, nil
}

// AddToList implements CustomListStore.
func (f *ListFile) AddToList(_ context.Context, uid, id string, t ListTitle) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.change(uid, func(s shelf) (bool, error) { return s.add(id, t, f.opts.now().UTC()) })
}

// RemoveFromList implements CustomListStore.
func (f *ListFile) RemoveFromList(_ context.Context, uid, id, kind string, tmdbID int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.change(uid, func(s shelf) (bool, error) { return s.remove(id, kind, tmdbID, f.opts.now().UTC()) })
}
