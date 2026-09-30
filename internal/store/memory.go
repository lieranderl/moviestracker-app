package store

import (
	"cmp"
	"context"
	"slices"
	"sync"
)

// Memory is a Store in this process's memory: gone when it stops.
type Memory struct {
	opts  options
	mu    sync.Mutex
	users map[string]*memoryUser
}

type memoryUser struct {
	prefs     Preferences
	favorites map[string]Favorite
}

// NewMemory returns an empty in-memory store.
func NewMemory(opts ...Option) *Memory {
	return &Memory{opts: newOptions(opts), users: map[string]*memoryUser{}}
}

// user is uid's data, created empty on first use. m.mu must be held.
func (m *Memory) user(uid string) *memoryUser {
	u, ok := m.users[uid]
	if !ok {
		u = &memoryUser{favorites: map[string]Favorite{}}
		m.users[uid] = u
	}
	return u
}

// Preferences implements Store.
func (m *Memory) Preferences(_ context.Context, uid string) (Preferences, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.user(uid).prefs, nil
}

// SavePreferences implements Store.
func (m *Memory) SavePreferences(_ context.Context, uid string, p Preferences) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.user(uid).prefs = p
	return nil
}

// Favorites implements Store.
func (m *Memory) Favorites(_ context.Context, uid string) ([]Favorite, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	favs := make([]Favorite, 0, len(m.user(uid).favorites))
	for _, f := range m.user(uid).favorites {
		favs = append(favs, f)
	}
	slices.SortFunc(favs, func(a, b Favorite) int { return b.AddedAt.Compare(a.AddedAt) })
	return favs, nil
}

// IsFavorite implements Store.
func (m *Memory) IsFavorite(_ context.Context, uid, kind string, tmdbID int) (bool, error) {
	id, err := favoriteID(kind, tmdbID)
	if err != nil {
		return false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.user(uid).favorites[id]
	return ok, nil
}

// AddFavorite implements Store.
func (m *Memory) AddFavorite(_ context.Context, uid string, f Favorite) error {
	id, err := favoriteID(f.Kind, f.TMDBID)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	favs := m.user(uid).favorites
	if _, ok := favs[id]; !ok {
		f.AddedAt = cmp.Or(f.AddedAt, m.opts.now()).UTC()
		favs[id] = f
	}
	return nil
}

// RemoveFavorite implements Store.
func (m *Memory) RemoveFavorite(_ context.Context, uid, kind string, tmdbID int) error {
	id, err := favoriteID(kind, tmdbID)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.user(uid).favorites, id)
	return nil
}
