package store

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
)

func (f *Firestore) episodeMappings(uid string) *firestore.CollectionRef {
	return f.userDoc(uid).Collection("episodeMappings")
}

// EpisodeMapping implements EpisodeMappingStore.
func (f *Firestore) EpisodeMapping(ctx context.Context, uid, hash string, index int) (EpisodeMapping, bool, error) {
	id, err := episodeMappingID(uid, hash, index)
	if err != nil {
		return EpisodeMapping{}, false, err
	}
	snap, err := f.episodeMappings(uid).Doc(id).Get(ctx)
	if notFound(err) {
		return EpisodeMapping{}, false, nil
	}
	if err != nil {
		return EpisodeMapping{}, false, fmt.Errorf("read episode mapping: %w", err)
	}
	var m EpisodeMapping
	if err := snap.DataTo(&m); err != nil {
		return EpisodeMapping{}, false, fmt.Errorf("read episode mapping: %w", err)
	}
	return servedEpisodeMapping(uid, m, true)
}

// SaveEpisodeMapping implements EpisodeMappingStore.
func (f *Firestore) SaveEpisodeMapping(ctx context.Context, uid string, m EpisodeMapping) error {
	m, id, err := checkEpisodeMapping(uid, m)
	if err != nil {
		return err
	}
	if err := f.accountSet(ctx, uid, f.episodeMappings(uid).Doc(id), m); err != nil {
		return fmt.Errorf("save episode mapping: %w", err)
	}
	return nil
}

// ClearEpisodeMapping implements EpisodeMappingStore.
func (f *Firestore) ClearEpisodeMapping(ctx context.Context, uid, hash string, index int) error {
	id, err := episodeMappingID(uid, hash, index)
	if err != nil {
		return err
	}
	if err := f.accountDelete(ctx, uid, f.episodeMappings(uid).Doc(id)); err != nil {
		return fmt.Errorf("clear episode mapping: %w", err)
	}
	return nil
}
