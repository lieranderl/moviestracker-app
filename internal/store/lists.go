package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// ListTitle is a title in a user's own list, as TMDB names it.
type ListTitle struct {
	Kind    string // "movie" or "tv"
	TMDBID  int
	Title   string
	Poster  string // TMDB image path
	AddedAt time.Time
}

// CustomList is a list a user names and fills with titles.
type CustomList struct {
	ID        string
	Name      string
	Titles    []ListTitle // newest added first
	ShareID   string      // its public link's ID; empty while not shared
	UpdatedAt time.Time
}

// CustomListStore keeps the lists each user makes. The local app keeps
// them in a file (OpenListFile), by account name.
type CustomListStore interface {
	// CustomLists are the user's lists, most recently changed first.
	CustomLists(ctx context.Context, uid string) ([]CustomList, error)
	// CreateCustomList makes an empty list named name, trimmed; a user
	// has at most maxLists.
	CreateCustomList(ctx context.Context, uid, name string) (CustomList, error)
	// RemoveCustomList deletes the user's list id.
	RemoveCustomList(ctx context.Context, uid, id string) error
	// CustomList is the user's list id; ok is false when they have none.
	CustomList(ctx context.Context, uid, id string) (l CustomList, ok bool, err error)
	// AddToList puts t first in the list, stamped now; a title already in
	// it stays where it is.
	AddToList(ctx context.Context, uid, id string, t ListTitle) error
	// RemoveFromList takes a title out of the list; one not in it is no
	// error.
	RemoveFromList(ctx context.Context, uid, id, kind string, tmdbID int) error
}

// ListSharing lets anyone with a list's link read it. Only the web app
// shares lists.
type ListSharing interface {
	// ShareList turns the link to the user's list id on, as a new random
	// link (one already on stays), or off, and returns the list.
	ShareList(ctx context.Context, uid, id string, on bool) (CustomList, error)
	// SharedList is the list whose link has shareID; ok is false when no
	// list is shared by it.
	SharedList(ctx context.Context, shareID string) (l CustomList, ok bool, err error)
}

// newShareID is a new list link's ID: 130 random bits, so links are not
// guessed.
func newShareID() string { return strings.ToLower(rand.Text()) }

var (
	// ErrNoList is why a change to a list the user does not have fails.
	ErrNoList = errors.New("no such list")
	// ErrTooManyLists is why a user with maxLists lists cannot make more.
	ErrTooManyLists = fmt.Errorf("a user keeps at most %d lists", maxLists)
	// ErrListFull is why a list of maxListTitles takes no more.
	ErrListFull = fmt.Errorf("a list holds at most %d titles", maxListTitles)
)

// How many lists a user may keep, and titles a list may hold: Firestore
// keeps a list's titles in its one document.
const (
	maxLists      = 50
	maxListTitles = 500
)

// maxListName is how many characters a list's name may have.
const maxListName = 60

// checkListName is name trimmed, or why it cannot name a list.
func checkListName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxListName {
		return "", fmt.Errorf("a list name has 1 to %d characters", maxListName)
	}
	return name, nil
}

// withTitle is l with t first, added at, and whether t was not in l yet.
func withTitle(l CustomList, t ListTitle, at time.Time) (CustomList, bool, error) {
	if slices.ContainsFunc(l.Titles, sameTitle(t.Kind, t.TMDBID)) {
		return l, false, nil
	}
	if len(l.Titles) >= maxListTitles {
		return l, false, ErrListFull
	}
	t.AddedAt = at
	l.Titles = append([]ListTitle{t}, l.Titles...)
	l.UpdatedAt = at
	return l, true, nil
}

// withoutTitle is l without a title, and whether it was in l.
func withoutTitle(l CustomList, kind string, tmdbID int, at time.Time) (CustomList, bool) {
	i := slices.IndexFunc(l.Titles, sameTitle(kind, tmdbID))
	if i < 0 {
		return l, false
	}
	l.Titles = slices.Delete(slices.Clone(l.Titles), i, i+1)
	l.UpdatedAt = at
	return l, true
}

// recentlyChanged orders lists most recently changed first.
func recentlyChanged(a, b CustomList) int { return b.UpdatedAt.Compare(a.UpdatedAt) }

func sameTitle(kind string, tmdbID int) func(ListTitle) bool {
	return func(t ListTitle) bool { return t.Kind == kind && t.TMDBID == tmdbID }
}

// shelf is one user's lists by ID, as Memory and ListFile keep them.
type shelf map[string]CustomList

func (s shelf) create(name string, at time.Time) (CustomList, error) {
	name, err := checkListName(name)
	if err != nil {
		return CustomList{}, err
	}
	if len(s) >= maxLists {
		return CustomList{}, ErrTooManyLists
	}
	l := CustomList{ID: newID(), Name: name, Titles: []ListTitle{}, UpdatedAt: at}
	s[l.ID] = l
	return l, nil
}

func (s shelf) get(id string) (CustomList, bool) {
	l, ok := s[id]
	l.Titles = slices.Clone(l.Titles)
	return l, ok
}

func (s shelf) all() []CustomList {
	out := make([]CustomList, 0, len(s))
	for id := range s {
		l, _ := s.get(id)
		out = append(out, l)
	}
	slices.SortFunc(out, recentlyChanged)
	return out
}

func (s shelf) delete(id string) error {
	if _, ok := s[id]; !ok {
		return ErrNoList
	}
	delete(s, id)
	return nil
}

// add puts t in list id, and says whether that changed it.
func (s shelf) add(id string, t ListTitle, at time.Time) (bool, error) {
	if _, err := titleID(t.Kind, t.TMDBID); err != nil {
		return false, err
	}
	l, ok := s[id]
	if !ok {
		return false, ErrNoList
	}
	l, changed, err := withTitle(l, t, at)
	s[id] = l
	return changed, err
}

// remove takes a title out of list id, and says whether that changed it.
func (s shelf) remove(id, kind string, tmdbID int, at time.Time) (bool, error) {
	l, ok := s[id]
	if !ok {
		return false, ErrNoList
	}
	l, changed := withoutTitle(l, kind, tmdbID, at)
	s[id] = l
	return changed, nil
}
