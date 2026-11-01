package store

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
)

// ProgressFile is a ProgressStore in one owner-only JSON file, replaced
// atomically on every change. Each user keeps their last recentWindow files.
type ProgressFile struct {
	opts  options
	path  string
	mu    sync.Mutex
	users map[string]map[string]Progress // by user, then by progressID
}

// OpenProgressFile reads the progress kept at path; a missing file is none.
func OpenProgressFile(path string, opts ...Option) (*ProgressFile, error) {
	f := &ProgressFile{opts: newOptions(opts), path: path, users: map[string]map[string]Progress{}}
	if err := readJSONFile(path, &f.users); err != nil {
		return nil, fmt.Errorf("read progress %s: %w", path, err)
	}
	return f, nil
}

// SaveProgress implements ProgressStore.
func (f *ProgressFile) SaveProgress(_ context.Context, uid string, p Progress) error {
	id, err := progressID(p.Hash, p.Index)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	files := f.users[uid]
	if files == nil {
		files = map[string]Progress{}
		f.users[uid] = files
	}
	p.UpdatedAt = f.opts.now().UTC()
	files[id] = p
	if len(files) > recentWindow {
		oldest := slices.MinFunc(slices.Collect(maps.Keys(files)), func(a, b string) int { return files[a].UpdatedAt.Compare(files[b].UpdatedAt) })
		delete(files, oldest)
	}
	return f.write()
}

// write replaces the file with f's progress. f.mu must be held.
func (f *ProgressFile) write() error {
	if err := writeJSONFile(f.path, f.users); err != nil {
		return fmt.Errorf("save progress: %w", err)
	}
	return nil
}

// Progress implements ProgressStore.
func (f *ProgressFile) Progress(_ context.Context, uid, hash string, index int) (Progress, bool, error) {
	id, err := progressID(hash, index)
	if err != nil {
		return Progress{}, false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.users[uid][id]
	return p, ok, nil
}

// RecentProgress implements ProgressStore.
func (f *ProgressFile) RecentProgress(_ context.Context, uid string, limit int) ([]Progress, error) {
	f.mu.Lock()
	all := slices.Collect(maps.Values(f.users[uid]))
	f.mu.Unlock()
	slices.SortFunc(all, func(a, b Progress) int { return b.UpdatedAt.Compare(a.UpdatedAt) })
	return unfinished(all, limit), nil
}
