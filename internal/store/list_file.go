package store

import (
	"context"
	"fmt"
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

func (f *ListFile) save() error {
	if err := writeJSONFile(f.path, f.users); err != nil {
		return fmt.Errorf("save lists: %w", err)
	}
	return nil
}

// CreateCustomList implements CustomListStore.
func (f *ListFile) CreateCustomList(_ context.Context, uid, name string) (CustomList, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	l, err := f.shelf(uid).create(name, f.opts.now().UTC())
	if err != nil {
		return CustomList{}, err
	}
	return l, f.save()
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
	if err := f.users[uid].delete(id); err != nil {
		return err
	}
	return f.save()
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
	changed, err := f.shelf(uid).add(id, t, f.opts.now().UTC())
	if err != nil || !changed {
		return err
	}
	return f.save()
}

// RemoveFromList implements CustomListStore.
func (f *ListFile) RemoveFromList(_ context.Context, uid, id, kind string, tmdbID int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	changed, err := f.shelf(uid).remove(id, kind, tmdbID, f.opts.now().UTC())
	if err != nil || !changed {
		return err
	}
	return f.save()
}
