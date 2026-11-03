package store

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"sync"
)

// fileDiscoveryPreset retains fields introduced by newer app versions.
type fileDiscoveryPreset struct {
	DiscoveryPreset
	raw map[string]json.RawMessage
}

func (p *fileDiscoveryPreset) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &p.DiscoveryPreset); err != nil {
		return err
	}
	return json.Unmarshal(data, &p.raw)
}

func (p fileDiscoveryPreset) MarshalJSON() ([]byte, error) {
	data, err := json.Marshal(p.DiscoveryPreset)
	if err != nil {
		return nil, err
	}
	var known map[string]json.RawMessage
	if err := json.Unmarshal(data, &known); err != nil {
		return nil, err
	}
	kept := maps.Clone(p.raw)
	if kept == nil {
		kept = map[string]json.RawMessage{}
	}
	// Filters may also gain fields while an older install is still in use.
	if original := kept["filters"]; len(original) > 0 {
		var filters, current map[string]json.RawMessage
		if err := json.Unmarshal(original, &filters); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(known["filters"], &current); err != nil {
			return nil, err
		}
		if filters == nil {
			filters = map[string]json.RawMessage{}
		}
		maps.Copy(filters, current)
		known["filters"], err = json.Marshal(filters)
		if err != nil {
			return nil, err
		}
	}
	maps.Copy(kept, known)
	return json.Marshal(kept)
}

// DiscoveryPresetFile persists presets in one owner-only atomic JSON file.
type DiscoveryPresetFile struct {
	opts  options
	path  string
	mu    sync.Mutex
	users map[string]map[string]fileDiscoveryPreset
}

// OpenDiscoveryPresetFile reads saved filters; a missing file is empty.
func OpenDiscoveryPresetFile(path string, opts ...Option) (*DiscoveryPresetFile, error) {
	f := &DiscoveryPresetFile{opts: newOptions(opts), path: path, users: map[string]map[string]fileDiscoveryPreset{}}
	if err := readJSONFile(path, &f.users); err != nil {
		return nil, fmt.Errorf("read discovery presets: %w", err)
	}
	if f.users == nil {
		f.users = map[string]map[string]fileDiscoveryPreset{}
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
	kept := make(map[string]DiscoveryPreset, len(f.users[uid]))
	for id, p := range f.users[uid] {
		kept[id] = p.DiscoveryPreset
	}
	return discoveryPresetList(kept), nil
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
		kept = map[string]fileDiscoveryPreset{}
	}
	kept[p.ID] = fileDiscoveryPreset{DiscoveryPreset: p}
	if err := f.save(uid, kept); err != nil {
		return DiscoveryPreset{}, err
	}
	return p, nil
}

// save replaces one user's presets only after the atomic write succeeds.
// The caller holds f.mu.
func (f *DiscoveryPresetFile) save(uid string, kept map[string]fileDiscoveryPreset) error {
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

// DeleteAccountPresets removes all saved filters before a local username can be reused.
// The local adapter fences this operation against authenticated preset requests.
func (f *DiscoveryPresetFile) DeleteAccountPresets(_ context.Context, uid string) error {
	if err := checkPresetOwner(uid); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.users[uid]) == 0 {
		return nil
	}
	return f.save(uid, nil)
}
