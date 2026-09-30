// Package store keeps what the cloud web app remembers for each signed-in
// user: their preferences and favourites. Firestore holds it in the cloud;
// the in-memory store is for tests and for local runs without Firestore.
//
// Firestore layout, in the moviestracker database (the Qwik app's catalog
// collections live beside it):
//
//	users/{uid}                        preferences and profile
//	users/{uid}/favorites/{kind}-{id}  one favourite title
//	users/{uid}/torrservers/{id}       one of the user's TorrServers
package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"strings"
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

	// TorrServers are the user's TorrServers, in the order added.
	TorrServers(ctx context.Context, uid string) ([]TorrServer, error)
	// SaveTorrServer keeps t: a new one (no ID) gets an ID, one with an ID
	// has its name and address changed. It returns t as kept.
	SaveTorrServer(ctx context.Context, uid string, t TorrServer) (TorrServer, error)
	// RemoveTorrServer forgets one; one not kept is no error.
	RemoveTorrServer(ctx context.Context, uid, id string) error
}

// TorrServer is a TorrServer a user streams from. Only its address is kept:
// its login stays in the user's browser.
type TorrServer struct {
	ID      string
	Name    string
	URL     string // http(s)://host[:port]
	AddedAt time.Time
}

// checkTorrServer is t with its name and address tidied, or why it cannot
// be kept: the address must be an http(s) URL with a host.
func checkTorrServer(t TorrServer) (TorrServer, error) {
	t.Name = strings.TrimSpace(t.Name)
	t.URL = strings.TrimRight(strings.TrimSpace(t.URL), "/")
	u, err := url.Parse(t.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return t, fmt.Errorf("TorrServer address %q is not an http(s) URL", t.URL)
	}
	if t.Name == "" {
		t.Name = u.Host
	}
	if len(t.Name) > 80 || len(t.URL) > 300 {
		return t, errors.New("TorrServer name or address is too long")
	}
	return t, nil
}

// newID is a new random document ID.
func newID() string { return strings.ToLower(rand.Text()[:16]) }

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
