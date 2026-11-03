package store

import (
	"context"
	"fmt"

	"cloud.google.com/go/firestore"
)

func readDiscoveryPresets(snap *firestore.DocumentSnapshot, err error) (map[string]DiscoveryPreset, error) {
	if notFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var rec struct {
		Presets map[string]DiscoveryPreset `firestore:"discoveryPresets"`
	}
	if err := snap.DataTo(&rec); err != nil {
		return nil, err
	}
	return rec.Presets, nil
}

// DiscoveryPresets implements DiscoveryPresetStore.
func (f *Firestore) DiscoveryPresets(ctx context.Context, uid string) ([]DiscoveryPreset, error) {
	if err := checkPresetOwner(uid); err != nil {
		return nil, err
	}
	kept, err := readDiscoveryPresets(f.userDoc(uid).Get(ctx))
	if err != nil {
		return nil, fmt.Errorf("read discovery presets: %w", err)
	}
	return discoveryPresetList(kept), nil
}

// CreateDiscoveryPreset implements DiscoveryPresetStore.
func (f *Firestore) CreateDiscoveryPreset(ctx context.Context, uid, name string, filters DiscoveryFilters) (DiscoveryPreset, error) {
	p, err := newDiscoveryPreset(uid, name, filters, f.opts.now())
	if err != nil {
		return DiscoveryPreset{}, err
	}
	err = f.accountTransaction(ctx, uid, func(_ context.Context, tx *firestore.Transaction) error {
		kept, err := readDiscoveryPresets(tx.Get(f.userDoc(uid)))
		if err != nil {
			return err
		}
		if len(kept) >= MaxDiscoveryPresets {
			return ErrPresetLimit
		}
		return tx.Set(f.userDoc(uid), map[string]any{"discoveryPresets": map[string]any{p.ID: p}}, firestore.Merge([]string{"discoveryPresets", p.ID}))
	})
	if err != nil {
		return DiscoveryPreset{}, fmt.Errorf("save discovery preset: %w", err)
	}
	return p, nil
}

// RenameDiscoveryPreset implements DiscoveryPresetStore.
func (f *Firestore) RenameDiscoveryPreset(ctx context.Context, uid, id, name string) error {
	if err := checkPresetOwner(uid); err != nil {
		return err
	}
	name, err := presetName(name)
	if err != nil {
		return err
	}
	err = f.accountTransaction(ctx, uid, func(_ context.Context, tx *firestore.Transaction) error {
		kept, err := readDiscoveryPresets(tx.Get(f.userDoc(uid)))
		if err != nil {
			return err
		}
		if _, ok := kept[id]; !ok {
			return ErrPresetNotFound
		}
		return tx.Update(f.userDoc(uid), []firestore.Update{{FieldPath: []string{"discoveryPresets", id, "name"}, Value: name}})
	})
	if err != nil {
		return fmt.Errorf("rename discovery preset: %w", err)
	}
	return nil
}

// DeleteDiscoveryPreset implements DiscoveryPresetStore.
func (f *Firestore) DeleteDiscoveryPreset(ctx context.Context, uid, id string) error {
	if err := checkPresetOwner(uid); err != nil {
		return err
	}
	err := f.accountTransaction(ctx, uid, func(_ context.Context, tx *firestore.Transaction) error {
		kept, err := readDiscoveryPresets(tx.Get(f.userDoc(uid)))
		if err != nil {
			return err
		}
		if _, ok := kept[id]; !ok {
			return nil
		}
		return tx.Update(f.userDoc(uid), []firestore.Update{{FieldPath: []string{"discoveryPresets", id}, Value: firestore.Delete}})
	})
	if err != nil {
		return fmt.Errorf("delete discovery preset: %w", err)
	}
	return nil
}
