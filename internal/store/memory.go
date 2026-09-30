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
	prefs       Preferences
	favorites   map[string]Favorite
	torrServers []TorrServer // in the order added
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

// TorrServers implements Store.
func (m *Memory) TorrServers(_ context.Context, uid string) ([]TorrServer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.user(uid).torrServers), nil
}

// SaveTorrServer implements Store.
func (m *Memory) SaveTorrServer(_ context.Context, uid string, t TorrServer) (TorrServer, error) {
	t, err := checkTorrServer(t)
	if err != nil {
		return TorrServer{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	u := m.user(uid)
	if i := slices.IndexFunc(u.torrServers, func(s TorrServer) bool { return s.ID == t.ID }); t.ID != "" && i >= 0 {
		u.torrServers[i].Name, u.torrServers[i].URL = t.Name, t.URL
		return u.torrServers[i], nil
	}
	t.ID, t.AddedAt = cmp.Or(t.ID, newID()), m.opts.now().UTC()
	u.torrServers = append(u.torrServers, t)
	return t, nil
}

// RemoveTorrServer implements Store.
func (m *Memory) RemoveTorrServer(_ context.Context, uid, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u := m.user(uid)
	u.torrServers = slices.DeleteFunc(u.torrServers, func(s TorrServer) bool { return s.ID == id })
	return nil
}
