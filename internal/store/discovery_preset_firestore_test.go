package store_test

import (
	"context"
	"crypto/rand"
	"os"
	"testing"

	"cloud.google.com/go/firestore"
	"github.com/lieranderl/moviestracker-app/internal/store"
)

func TestPresetMutationsPreserveFutureStoredFields(t *testing.T) {
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Skip("requires Firestore emulator")
	}
	ctx := context.Background()
	s, err := store.NewFirestore(ctx, "demo-moviestracker", "moviestracker")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	client, err := firestore.NewClientWithDatabase(ctx, "demo-moviestracker", "moviestracker")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	uid := "future-preset-" + rand.Text()
	p, err := s.CreateDiscoveryPreset(ctx, uid, "Friday", store.DiscoveryFilters{Kind: "movie", Sort: "popular"})
	if err != nil {
		t.Fatal(err)
	}
	doc := client.Collection("users").Doc(uid)
	_, err = doc.Update(ctx, []firestore.Update{
		{FieldPath: []string{"discoveryPresets", p.ID, "futureField"}, Value: "kept"},
		{Path: "futurePreference", Value: "kept"},
	})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateDiscoveryPreset(ctx, uid, "Sunday", p.Filters)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RenameDiscoveryPreset(ctx, uid, p.ID, "Cinema night"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteDiscoveryPreset(ctx, uid, other.ID); err != nil {
		t.Fatal(err)
	}
	got, err := doc.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"discoveryPresets." + p.ID + ".futureField", "futurePreference"} {
		if value, err := got.DataAt(path); err != nil || value != "kept" {
			t.Errorf("future field %s = %v, %v; want kept", path, value, err)
		}
	}
}
