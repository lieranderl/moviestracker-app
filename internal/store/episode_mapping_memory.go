package store

import "context"

// EpisodeMapping implements EpisodeMappingStore.
func (m *Memory) EpisodeMapping(_ context.Context, uid, hash string, index int) (EpisodeMapping, bool, error) {
	id, err := episodeMappingID(uid, hash, index)
	if err != nil {
		return EpisodeMapping{}, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	kept, found := m.user(uid).mappings[id]
	return kept, found, nil
}

// SaveEpisodeMapping implements EpisodeMappingStore.
func (m *Memory) SaveEpisodeMapping(ctx context.Context, uid string, mapping EpisodeMapping) error {
	mapping, id, err := checkEpisodeMapping(uid, mapping)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.checkAccountSession(ctx, uid); err != nil {
		return err
	}
	u := m.user(uid)
	if u.mappings == nil {
		u.mappings = map[string]EpisodeMapping{}
	}
	u.mappings[id] = mapping
	return nil
}

// ClearEpisodeMapping implements EpisodeMappingStore.
func (m *Memory) ClearEpisodeMapping(ctx context.Context, uid, hash string, index int) error {
	id, err := episodeMappingID(uid, hash, index)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.checkAccountSession(ctx, uid); err != nil {
		return err
	}
	delete(m.user(uid).mappings, id)
	return nil
}
