// Package store keeps what the cloud web app remembers for each signed-in
// user: their preferences, favourites and where they stopped watching. Firestore holds it in the cloud;
// the in-memory store is for tests and for local runs without Firestore.
//
// Firestore layout, in the moviestracker database (the Qwik app's catalog
// collections live beside it):
//
//	users/{uid}                        preferences and profile
//	users/{uid}/favorites/{kind}-{id}  one favourite title
//	users/{uid}/torrservers/{id}       one of the user's TorrServers
//	users/{uid}/progress/{hash}-{file} where they stopped in one file
//	users/{uid}/titles/{kind}-{id}     a title they want to watch or watched
//	users/{uid}/updates/{feed}-{id}    a release they were told about
//	users/{uid}/lists/{id}             a list the user made, with its titles
//	sharedLists/{shareId}              whose list a public link opens
//	users/{uid}/reviews/{kind}-{id}    a user's rating of a title and note on it
package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strconv"
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
	// FavoritesPage is a page of the user's favourites, newest first,
	// starting after the one the cursor after names ("" for the newest).
	// next is the following page's cursor, "" when none is left.
	FavoritesPage(ctx context.Context, uid, after string) (page []Favorite, next string, err error)

	// TorrServers are the user's TorrServers, in the order added.
	TorrServers(ctx context.Context, uid string) ([]TorrServer, error)
	// SaveTorrServer keeps t: a new one (no ID) gets an ID, one with an ID
	// has its name and address changed. It returns t as kept.
	SaveTorrServer(ctx context.Context, uid string, t TorrServer) (TorrServer, error)
	// RemoveTorrServer forgets one; one not kept is no error.
	RemoveTorrServer(ctx context.Context, uid, id string) error

	ProgressStore
	TitleStateStore
	UpdateStore
	ReleaseFilterStore
	CustomListStore
	ListSharing
	ReviewStore
}

// ReleaseFilters are how a user last searched a title's releases.
type ReleaseFilters struct {
	Sort           string // "seeders", "date", "size" or "bitrate"; empty: seeders
	Qualities      []int  // of 2160, 1080, 720, 480; none is all
	HDR            bool
	HideRecordings bool // camera and telesync recordings of a screening
}

// ReleaseFilterStore keeps each user's release filters. The local app keeps
// them in a file (OpenFilterFile), by account name.
type ReleaseFilterStore interface {
	ReleaseFilters(ctx context.Context, uid string) (ReleaseFilters, error)
	SaveReleaseFilters(ctx context.Context, uid string, f ReleaseFilters) error
}

// CheckReleaseFilters is why f cannot be kept, if it cannot.
func CheckReleaseFilters(f ReleaseFilters) error {
	if !slices.Contains([]string{"", "seeders", "date", "size", "bitrate"}, f.Sort) {
		return fmt.Errorf("release sort %q is unknown", f.Sort)
	}
	for _, q := range f.Qualities {
		if !slices.Contains([]int{2160, 1080, 720, 480}, q) {
			return fmt.Errorf("release quality %d is unknown", q)
		}
	}
	return nil
}

// Update tells a user a movie they follow turned up in one of the
// backend's release feeds (internal/releases). They are told once per feed.
type Update struct {
	Feed    string // the feed's Firestore collection
	TMDBID  int
	Title   string
	Poster  string    // TMDB image path
	FoundAt time.Time // when the backend found it, as the feed said then
	Seen    bool
}

// updateFeeds are the release feeds updates come from.
var updateFeeds = []string{"latesttorrentsmovies", "hdr10movies", "dvmovies"}

// updateLimit is how many updates a page of Updates holds.
const updateLimit = 100

// UpdateStore keeps what users were told about new releases.
type UpdateStore interface {
	// AddUpdates keeps updates the user was not told about yet; one already
	// kept stays as it was, seen or not.
	AddUpdates(ctx context.Context, uid string, updates []Update) error
	// Updates are a page of the user's updates, most recently found first,
	// starting after the one the cursor after names ("" for the newest).
	// next is the cursor of the following page, "" when none is left.
	Updates(ctx context.Context, uid, after string) (page []Update, next string, err error)
	// UnseenUpdates counts the user's updates not seen yet.
	UnseenUpdates(ctx context.Context, uid string) (int, error)
	// MarkUpdatesSeen marks the user's updates with these IDs (Update.ID)
	// seen; IDs the user has no update for are ignored.
	MarkUpdatesSeen(ctx context.Context, uid string, ids []string) error
	// MarkAllUpdatesSeen marks the user's updates found up to through seen;
	// ones found later stay as they are.
	MarkAllUpdatesSeen(ctx context.Context, uid string, through time.Time) error
}

// ID names the update among the user's: its feed and TMDB ID
// ("dvmovies-438631").
func (u Update) ID() string { return fmt.Sprintf("%s-%d", u.Feed, u.TMDBID) }

// updateCursor names the update a page ends with.
func updateCursor(u Update) string { return pageCursor(u.FoundAt, u.ID()) }

// pageCursor names the record a page ends with: its time and key
// ("1790000000000000000.dvmovies-438631"). Pages are ordered by time and
// then key, both descending.
func pageCursor(at time.Time, key string) string { return fmt.Sprintf("%d.%s", at.UnixNano(), key) }

// ErrCursor is a page cursor the store did not make.
var ErrCursor = errors.New("page cursor is malformed")

// parseCursor reads a cursor pageCursor made.
func parseCursor(cursor string) (time.Time, string, error) {
	at, id, ok := strings.Cut(cursor, ".")
	nanos, err := strconv.ParseInt(at, 10, 64)
	if !ok || err != nil || id == "" {
		return time.Time{}, "", fmt.Errorf("%w: %q", ErrCursor, cursor)
	}
	return time.Unix(0, nanos).UTC(), id, nil
}

// libraryPageSize is how many titles a page of favourites or of a watch
// list holds.
const libraryPageSize = 60

// pageOf is the page of items following the cursor after, size at most,
// and the next page's cursor. at and key order items newest first, then
// by key descending, as Firestore pages them.
func pageOf[T any](items []T, at func(T) time.Time, key func(T) string, after string, size int) ([]T, string, error) {
	newest := func(a, b T) int {
		if c := at(b).Compare(at(a)); c != 0 {
			return c
		}
		return strings.Compare(key(b), key(a))
	}
	items = slices.SortedFunc(slices.Values(items), newest)
	if after != "" {
		t, k, err := parseCursor(after)
		if err != nil {
			return nil, "", err
		}
		items = slices.DeleteFunc(items, func(it T) bool {
			return at(it).After(t) || (at(it).Equal(t) && key(it) >= k)
		})
	}
	if len(items) <= size {
		return items, "", nil
	}
	last := items[size-1]
	return items[:size], pageCursor(at(last), key(last)), nil
}

func favoriteAt(f Favorite) time.Time { return f.AddedAt }
func favoriteKey(f Favorite) string   { return fmt.Sprintf("%s-%d", f.Kind, f.TMDBID) }
func titleAt(t TitleState) time.Time  { return t.UpdatedAt }
func titleKey(t TitleState) string    { return fmt.Sprintf("%s-%d", t.Kind, t.TMDBID) }

// updateID is an update's key: its feed and TMDB ID ("dvmovies-438631").
func updateID(u Update) (string, error) {
	if !slices.Contains(updateFeeds, u.Feed) {
		return "", fmt.Errorf("release feed %q is unknown", u.Feed)
	}
	if u.TMDBID <= 0 {
		return "", fmt.Errorf("update TMDB ID %d is not positive", u.TMDBID)
	}
	return fmt.Sprintf("%s-%d", u.Feed, u.TMDBID), nil
}

// newestFound orders updates most recently found first, then by ID
// descending, so updates found at the same moment keep one order.
func newestFound(a, b Update) int {
	if c := b.FoundAt.Compare(a.FoundAt); c != 0 {
		return c
	}
	return strings.Compare(b.ID(), a.ID())
}

// Watch states a user gives a title.
const (
	WantToWatch = "want"
	Watched     = "watched"
)

// TitleState is a title a user wants to watch or has watched, as TMDB names
// it. A title has one state: watching it takes it off Want to watch.
type TitleState struct {
	Kind      string // "movie" or "tv"
	TMDBID    int
	Title     string
	Poster    string // TMDB image path
	State     string // WantToWatch or Watched; empty: neither
	UpdatedAt time.Time
}

// TitleStateStore keeps each user's watch states. The local app keeps them
// in a file (OpenTitleFile), by account name.
type TitleStateStore interface {
	// SetTitleState gives a title its state, stamped now; an empty state
	// forgets it.
	SetTitleState(ctx context.Context, uid string, ts TitleState) error
	// TitleState is the title's state; an empty State when it has none.
	TitleState(ctx context.Context, uid, kind string, tmdbID int) (TitleState, error)
	// TitleStates are the user's titles in state, most recently marked first.
	TitleStates(ctx context.Context, uid, state string) ([]TitleState, error)
	// TitleStatesPage is a page of them, starting after the one the cursor
	// after names ("" for the newest); next is the following page's
	// cursor, "" when none is left.
	TitleStatesPage(ctx context.Context, uid, state, after string) (page []TitleState, next string, err error)
}

// checkTitleState is why ts cannot be kept, if it cannot; its key otherwise.
func checkTitleState(ts TitleState) (string, error) {
	if ts.State != "" && ts.State != WantToWatch && ts.State != Watched {
		return "", fmt.Errorf("watch state %q is not %q or %q", ts.State, WantToWatch, Watched)
	}
	return titleID(ts.Kind, ts.TMDBID)
}

// newestFirst orders title states most recently marked first.
func newestFirst(a, b TitleState) int { return b.UpdatedAt.Compare(a.UpdatedAt) }

// ProgressStore keeps where each user stopped watching. The local app keeps
// it in a file (OpenProgressFile), by account name.
type ProgressStore interface {
	// SaveProgress keeps where the user is in one file, stamped now.
	SaveProgress(ctx context.Context, uid string, p Progress) error
	// Progress is where the user stopped in a torrent's file, if anywhere.
	Progress(ctx context.Context, uid, hash string, index int) (Progress, bool, error)
	// RecentProgress is up to limit files the user has not finished,
	// most recently watched first, from their last recentWindow files.
	RecentProgress(ctx context.Context, uid string, limit int) ([]Progress, error)
}

// recentWindow is how many recently watched files RecentProgress looks
// through: finished ones are skipped there, so no Firestore index is needed.
const recentWindow = 200

// Progress is where a user stopped in one file of a torrent. The title,
// poster and TMDB title (if TorrServer knows it) say what it is.
type Progress struct {
	Hash      string // the torrent's info hash
	Index     int    // the file's TorrServer ID
	File      string // the file's path in the torrent
	Title     string
	Poster    string // TMDB image path or an http(s) URL
	Kind      string // "movie" or "tv"; empty when unknown
	TMDBID    int
	Position  time.Duration
	Duration  time.Duration
	UpdatedAt time.Time
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

// titleID is a title's key: its kind and TMDB ID ("movie-438631").
func titleID(kind string, tmdbID int) (string, error) {
	if kind != "movie" && kind != "tv" {
		return "", fmt.Errorf("title kind %q is not movie or tv", kind)
	}
	if tmdbID <= 0 {
		return "", fmt.Errorf("title TMDB ID %d is not positive", tmdbID)
	}
	return fmt.Sprintf("%s-%d", kind, tmdbID), nil
}

// Finished reports whether p is watched to the end: 92% of it, or all but
// its last three minutes (the credits).
func (p Progress) Finished() bool {
	return p.Duration > 0 && (p.Position*100 >= p.Duration*92 || p.Duration-p.Position <= 3*time.Minute)
}

// progressID is a file's key: its torrent's hash and its ID ("0123…-1").
func progressID(hash string, index int) (string, error) {
	if len(hash) != 40 || strings.Trim(strings.ToLower(hash), "0123456789abcdef") != "" {
		return "", fmt.Errorf("torrent hash %q is not 40 hex digits", hash)
	}
	if index <= 0 {
		return "", fmt.Errorf("file ID %d is not positive", index)
	}
	return fmt.Sprintf("%s-%d", strings.ToLower(hash), index), nil
}

// unfinished is up to limit of ps that are not finished, in order.
func unfinished(ps []Progress, limit int) []Progress {
	out := []Progress{}
	for _, p := range ps {
		if len(out) == limit {
			break
		}
		if !p.Finished() {
			out = append(out, p)
		}
	}
	return out
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
