package store

import (
	"cmp"
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// DiscoveryFilters are the durable fields of a Discover query.
type DiscoveryFilters struct {
	Kind       string  `json:"type" firestore:"type"`
	Genre      int     `json:"genre" firestore:"genre"`
	Year       int     `json:"year" firestore:"year"`
	MinRating  float64 `json:"rating" firestore:"rating"`
	Sort       string  `json:"sort" firestore:"sort"`
	MaxRuntime int     `json:"runtime" firestore:"runtime"`
	Language   string  `json:"language" firestore:"language"`
}

// DiscoveryPreset is a named set of filters owned by one account.
type DiscoveryPreset struct {
	ID        string           `json:"id" firestore:"id"`
	Name      string           `json:"name" firestore:"name"`
	Filters   DiscoveryFilters `json:"filters" firestore:"filters"`
	CreatedAt time.Time        `json:"createdAt" firestore:"createdAt"`
}

// DiscoveryPresetStore keeps account-owned Discover filters.
type DiscoveryPresetStore interface {
	DiscoveryPresets(ctx context.Context, uid string) ([]DiscoveryPreset, error)
	CreateDiscoveryPreset(ctx context.Context, uid, name string, filters DiscoveryFilters) (DiscoveryPreset, error)
	RenameDiscoveryPreset(ctx context.Context, uid, id, name string) error
	DeleteDiscoveryPreset(ctx context.Context, uid, id string) error
}

// ErrPresetNotFound means the account does not own the requested preset.
var ErrPresetNotFound = errors.New("store: discovery preset not found")

// MaxDiscoveryPresets bounds each account's saved filters.
const MaxDiscoveryPresets = 20

// ErrPresetLimit means the account must remove a preset before creating another.
var ErrPresetLimit = errors.New("store: too many discovery presets")

// ErrInvalidPreset means the supplied account, name or filters cannot be saved.
var ErrInvalidPreset = errors.New("store: invalid discovery preset")

func checkPresetOwner(uid string) error {
	if strings.TrimSpace(uid) == "" || strings.Contains(uid, "/") {
		return ErrInvalidPreset
	}
	return nil
}

func presetName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || name == "" || utf8.RuneCountInString(name) > 60 || strings.ContainsFunc(name, unicode.IsControl) {
		return "", ErrInvalidPreset
	}
	return name, nil
}

func newDiscoveryPreset(uid, name string, filters DiscoveryFilters, at time.Time) (DiscoveryPreset, error) {
	if err := checkPresetOwner(uid); err != nil {
		return DiscoveryPreset{}, err
	}
	name, err := presetName(name)
	if err != nil {
		return DiscoveryPreset{}, err
	}
	if filters.Kind != "movie" && filters.Kind != "tv" || filters.Genre < 0 || filters.Year < 0 || filters.Year > 9999 ||
		math.IsNaN(filters.MinRating) || filters.MinRating < 0 || filters.MinRating > 10 || filters.MaxRuntime < 0 || filters.MaxRuntime > 1440 {
		return DiscoveryPreset{}, ErrInvalidPreset
	}
	if filters.Language != "" && (len(filters.Language) != 2 || strings.ContainsFunc(filters.Language, func(r rune) bool { return r < 'a' || r > 'z' })) {
		return DiscoveryPreset{}, ErrInvalidPreset
	}
	switch filters.Sort {
	case "", "popular", "rating", "newest":
	default:
		return DiscoveryPreset{}, ErrInvalidPreset
	}
	return DiscoveryPreset{ID: newID(), Name: name, Filters: filters, CreatedAt: at.UTC()}, nil
}

func discoveryPresetList(kept map[string]DiscoveryPreset) []DiscoveryPreset {
	out := make([]DiscoveryPreset, 0, len(kept))
	for _, p := range kept {
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b DiscoveryPreset) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), strings.Compare(a.ID, b.ID))
	})
	return out
}
