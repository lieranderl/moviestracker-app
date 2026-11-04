package store

import (
	"context"
	"fmt"
	"maps"
	"sync"
)

// EpisodeMappingFile retains confirmed files in an owner-only JSON file.
type EpisodeMappingFile struct {
	path  string
	mu    sync.Mutex
	users map[string]map[string]EpisodeMapping
}

// OpenEpisodeMappingFile opens saved confirmations; a missing file is empty.
func OpenEpisodeMappingFile(path string) (*EpisodeMappingFile, error) {
	f := &EpisodeMappingFile{path: path, users: map[string]map[string]EpisodeMapping{}}
	if err := readJSONFile(path, &f.users); err != nil {
		return nil, fmt.Errorf("read episode mappings: %w", err)
	}
	if f.users == nil {
		f.users = map[string]map[string]EpisodeMapping{}
	}
	return f, nil
}

// EpisodeMapping implements EpisodeMappingStore.
func (f *EpisodeMappingFile) EpisodeMapping(_ context.Context, uid, hash string, index int) (EpisodeMapping, bool, error) {
	id, err := episodeMappingID(uid, hash, index)
	if err != nil {
		return EpisodeMapping{}, false, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	m, found := f.users[uid][id]
	return servedEpisodeMapping(uid, m, found)
}

// SaveEpisodeMapping implements EpisodeMappingStore.
func (f *EpisodeMappingFile) SaveEpisodeMapping(_ context.Context, uid string, m EpisodeMapping) error {
	m, id, err := checkEpisodeMapping(uid, m)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := maps.Clone(f.users[uid])
	if kept == nil {
		kept = map[string]EpisodeMapping{}
	}
	kept[id] = m
	return f.save(uid, kept)
}

// ClearEpisodeMapping implements EpisodeMappingStore.
func (f *EpisodeMappingFile) ClearEpisodeMapping(_ context.Context, uid, hash string, index int) error {
	id, err := episodeMappingID(uid, hash, index)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, found := f.users[uid][id]; !found {
		return nil
	}
	kept := maps.Clone(f.users[uid])
	delete(kept, id)
	return f.save(uid, kept)
}

// DeleteAccountMappings removes confirmations before a local account name is reused.
// The authenticated adapter must fence this with account creation/deletion.
func (f *EpisodeMappingFile) DeleteAccountMappings(_ context.Context, uid string) error {
	if err := checkMappingOwner(uid); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.users[uid]) == 0 {
		return nil
	}
	return f.save(uid, nil)
}

// save commits live state only after the owner-only replacement succeeds.
// f.mu must be held and kept must not alias the current user's map.
func (f *EpisodeMappingFile) save(uid string, kept map[string]EpisodeMapping) error {
	users := maps.Clone(f.users)
	if len(kept) == 0 {
		delete(users, uid)
	} else {
		users[uid] = kept
	}
	if err := writeJSONFile(f.path, users); err != nil {
		return fmt.Errorf("save episode mappings: %w", err)
	}
	f.users = users
	return nil
}
