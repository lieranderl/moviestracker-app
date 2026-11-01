package store

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

// FilterFile is a ReleaseFilterStore in one owner-only JSON file, replaced
// atomically on every change.
type FilterFile struct {
	path  string
	mu    sync.Mutex
	users map[string]ReleaseFilters
}

// OpenFilterFile reads the release filters kept at path; a missing file is
// none.
func OpenFilterFile(path string) (*FilterFile, error) {
	f := &FilterFile{path: path, users: map[string]ReleaseFilters{}}
	if err := readJSONFile(path, &f.users); err != nil {
		return nil, fmt.Errorf("read release filters %s: %w", path, err)
	}
	return f, nil
}

// ReleaseFilters implements ReleaseFilterStore.
func (f *FilterFile) ReleaseFilters(_ context.Context, uid string) (ReleaseFilters, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	filters := f.users[uid]
	filters.Qualities = slices.Clone(filters.Qualities)
	return filters, nil
}

// SaveReleaseFilters implements ReleaseFilterStore.
func (f *FilterFile) SaveReleaseFilters(_ context.Context, uid string, filters ReleaseFilters) error {
	if err := CheckReleaseFilters(filters); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	filters.Qualities = slices.Clone(filters.Qualities)
	f.users[uid] = filters
	if err := writeJSONFile(f.path, f.users); err != nil {
		return fmt.Errorf("save release filters: %w", err)
	}
	return nil
}
