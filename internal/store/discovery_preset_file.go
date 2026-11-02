package store

import (
	"context"
	"fmt"
	"maps"
	"sync"
)

// DiscoveryPresetFile persists presets in one owner-only atomic JSON file.
type DiscoveryPresetFile struct {
	opts  options
	path  string
	mu    sync.Mutex
	users map[string]map[string]DiscoveryPreset
}

// OpenDiscoveryPresetFile reads saved filters; a missing file is empty.
func OpenDiscoveryPresetFile(path string, opts ...Option) (*DiscoveryPresetFile, error) {
	f := &DiscoveryPresetFile{opts: newOptions(opts), path: path, users: map[string]map[string]DiscoveryPreset{}}
	if err := readJSONFile(path, &f.users); err != nil {
		return nil, fmt.Errorf("read discovery presets: %w", err)
	}
	return f, nil
}

// DiscoveryPresets implements DiscoveryPresetStore.
func (f *DiscoveryPresetFile) DiscoveryPresets(_ context.Context, uid string) ([]DiscoveryPreset, error) {
	if err := checkPresetOwner(uid); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return discoveryPresetList(f.users[uid]), nil
}

// CreateDiscoveryPreset implements DiscoveryPresetStore.
func (f *DiscoveryPresetFile) CreateDiscoveryPreset(_ context.Context, uid, name string, filters DiscoveryFilters) (DiscoveryPreset, error) {
	p, err := newDiscoveryPreset(uid, name, filters, f.opts.now())
	if err != nil {
		return DiscoveryPreset{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.users[uid]) >= MaxDiscoveryPresets {
		return DiscoveryPreset{}, ErrPresetLimit
	}
	before := f.users[uid]
	kept := maps.Clone(before)
	if kept == nil {
		kept = map[string]DiscoveryPreset{}
	}
	kept[p.ID] = p
	if err := f.save(uid, kept); err != nil {
		return DiscoveryPreset{}, err
	}
	return p, nil
}

// save replaces one user's presets only after the atomic write succeeds.
// The caller holds f.mu.
func (f *DiscoveryPresetFile) save(uid string, kept map[string]DiscoveryPreset) error {
	before := f.users[uid]
	f.users[uid] = kept
	if err := writeJSONFile(f.path, f.users); err != nil {
		f.users[uid] = before
		return fmt.Errorf("save discovery presets: %w", err)
	}
	return nil
}

// RenameDiscoveryPreset implements DiscoveryPresetStore.
func (f *DiscoveryPresetFile) RenameDiscoveryPreset(_ context.Context, uid, id, name string) error {
	if err := checkPresetOwner(uid); err != nil {
		return err
	}
	name, err := presetName(name)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.users[uid][id]
	if !ok {
		return ErrPresetNotFound
	}
	p.Name = name
	kept := maps.Clone(f.users[uid])
	kept[id] = p
	return f.save(uid, kept)
}

// DeleteDiscoveryPreset implements DiscoveryPresetStore.
func (f *DiscoveryPresetFile) DeleteDiscoveryPreset(_ context.Context, uid, id string) error {
	if err := checkPresetOwner(uid); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.users[uid][id]; !ok {
		return nil
	}
	kept := maps.Clone(f.users[uid])
	delete(kept, id)
	return f.save(uid, kept)
}
