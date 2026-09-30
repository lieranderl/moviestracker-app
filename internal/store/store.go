// Package store keeps what the cloud web app remembers for each signed-in
// user: their preferences and favourites. Firestore holds it in the cloud;
// the in-memory store is for tests and for local runs without Firestore.
//
// Firestore layout, in the moviestracker database (the Qwik app's catalog
// collections live beside it):
//
//	users/{uid}                        preferences and profile
//	users/{uid}/favorites/{kind}-{id}  one favourite title
package store

import (
	"context"
	"fmt"
	"time"
)

// Preferences are a user's settings.
type Preferences struct {
	Language string // interface language ("en", "ru"); empty: the browser's
}

// Favorite is a title a user keeps, as TMDB names it.
type Favorite struct {
	Kind    string // "movie" or "tv"
	TMDBID  int
	Title   string
	Poster  string // TMDB image path
	AddedAt time.Time
}

// Store keeps each user's data, by their Google account ID.
type Store interface {
	Preferences(ctx context.Context, uid string) (Preferences, error)
	SavePreferences(ctx context.Context, uid string, p Preferences) error

	// Favorites are the user's favourites, newest first.
	Favorites(ctx context.Context, uid string) ([]Favorite, error)
	IsFavorite(ctx context.Context, uid, kind string, tmdbID int) (bool, error)
	// AddFavorite keeps f, stamped now; one already kept stays as it was.
	AddFavorite(ctx context.Context, uid string, f Favorite) error
	// RemoveFavorite forgets a favourite; one not kept is no error.
	RemoveFavorite(ctx context.Context, uid, kind string, tmdbID int) error
}

// favoriteID is a favourite's key: its kind and TMDB ID ("movie-438631").
func favoriteID(kind string, tmdbID int) (string, error) {
	if kind != "movie" && kind != "tv" {
		return "", fmt.Errorf("favourite kind %q is not movie or tv", kind)
	}
	if tmdbID <= 0 {
		return "", fmt.Errorf("favourite TMDB ID %d is not positive", tmdbID)
	}
	return fmt.Sprintf("%s-%d", kind, tmdbID), nil
}

// Option configures a store.
type Option func(*options)

type options struct {
	now func() time.Time
}

// WithClock sets the clock that stamps when things happen.
func WithClock(now func() time.Time) Option {
	return func(o *options) { o.now = now }
}

func newOptions(opts []Option) options {
	o := options{now: time.Now}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}
