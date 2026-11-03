package store

import (
	"context"
)

// DiscoveryPresets implements DiscoveryPresetStore.
func (m *Memory) DiscoveryPresets(_ context.Context, uid string) ([]DiscoveryPreset, error) {
	if err := checkPresetOwner(uid); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return discoveryPresetList(m.user(uid).presets), nil
}

// CreateDiscoveryPreset implements DiscoveryPresetStore.
func (m *Memory) CreateDiscoveryPreset(ctx context.Context, uid, name string, filters DiscoveryFilters) (DiscoveryPreset, error) {
	p, err := newDiscoveryPreset(uid, name, filters, m.opts.now())
	if err != nil {
		return DiscoveryPreset{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.checkAccountSession(ctx, uid); err != nil {
		return DiscoveryPreset{}, err
	}
	u := m.user(uid)
	if len(u.presets) >= MaxDiscoveryPresets {
		return DiscoveryPreset{}, ErrPresetLimit
	}
	if u.presets == nil {
		u.presets = map[string]DiscoveryPreset{}
	}
	u.presets[p.ID] = p
	return p, nil
}

// RenameDiscoveryPreset implements DiscoveryPresetStore.
func (m *Memory) RenameDiscoveryPreset(ctx context.Context, uid, id, name string) error {
	if err := checkPresetOwner(uid); err != nil {
		return err
	}
	name, err := presetName(name)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.checkAccountSession(ctx, uid); err != nil {
		return err
	}
	u := m.user(uid)
	p, ok := u.presets[id]
	if !ok {
		return ErrPresetNotFound
	}
	p.Name = name
	u.presets[id] = p
	return nil
}

// DeleteDiscoveryPreset implements DiscoveryPresetStore.
func (m *Memory) DeleteDiscoveryPreset(ctx context.Context, uid, id string) error {
	if err := checkPresetOwner(uid); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.checkAccountSession(ctx, uid); err != nil {
		return err
	}
	delete(m.user(uid).presets, id)
	return nil
}
