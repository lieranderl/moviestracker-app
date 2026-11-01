package store

import (
	"cmp"
	"context"
	"maps"
	"slices"
	"sync"
)

// Memory is a Store in this process's memory: gone when it stops.
type Memory struct {
	opts   options
	mu     sync.Mutex
	users  map[string]*memoryUser
	shared map[string]sharedList // by share ID
}

// sharedList is whose list a link opens.
type sharedList struct{ uid, id string }

type memoryUser struct {
	prefs       Preferences
	favorites   map[string]Favorite
	torrServers []TorrServer // in the order added
	progress    map[string]Progress
	titles      map[string]TitleState
	updates     map[string]Update
	filters     ReleaseFilters
	lists       shelf
	reviews     map[string]Review
}

// NewMemory returns an empty in-memory store.
func NewMemory(opts ...Option) *Memory {
	return &Memory{opts: newOptions(opts), users: map[string]*memoryUser{}, shared: map[string]sharedList{}}
}

// user is uid's data, created empty on first use. m.mu must be held.
func (m *Memory) user(uid string) *memoryUser {
	u, ok := m.users[uid]
	if !ok {
		u = &memoryUser{favorites: map[string]Favorite{}, progress: map[string]Progress{}, titles: map[string]TitleState{}, updates: map[string]Update{}, lists: shelf{}, reviews: map[string]Review{}}
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
	id, err := titleID(kind, tmdbID)
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
	id, err := titleID(f.Kind, f.TMDBID)
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
	id, err := titleID(kind, tmdbID)
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

// SaveProgress implements Store.
func (m *Memory) SaveProgress(_ context.Context, uid string, p Progress) error {
	id, err := progressID(p.Hash, p.Index)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p.UpdatedAt = m.opts.now().UTC()
	m.user(uid).progress[id] = p
	return nil
}

// Progress implements Store.
func (m *Memory) Progress(_ context.Context, uid, hash string, index int) (Progress, bool, error) {
	id, err := progressID(hash, index)
	if err != nil {
		return Progress{}, false, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.user(uid).progress[id]
	return p, ok, nil
}

// RecentProgress implements Store.
func (m *Memory) RecentProgress(_ context.Context, uid string, limit int) ([]Progress, error) {
	m.mu.Lock()
	all := slices.Collect(maps.Values(m.user(uid).progress))
	m.mu.Unlock()
	slices.SortFunc(all, func(a, b Progress) int { return b.UpdatedAt.Compare(a.UpdatedAt) })
	return unfinished(all[:min(len(all), recentWindow)], limit), nil
}

// SetTitleState implements Store.
func (m *Memory) SetTitleState(_ context.Context, uid string, ts TitleState) error {
	id, err := checkTitleState(ts)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if ts.State == "" {
		delete(m.user(uid).titles, id)
		return nil
	}
	ts.UpdatedAt = m.opts.now().UTC()
	m.user(uid).titles[id] = ts
	return nil
}

// TitleState implements Store.
func (m *Memory) TitleState(_ context.Context, uid, kind string, tmdbID int) (TitleState, error) {
	id, err := titleID(kind, tmdbID)
	if err != nil {
		return TitleState{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.user(uid).titles[id], nil
}

// TitleStates implements Store.
func (m *Memory) TitleStates(_ context.Context, uid, state string) ([]TitleState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []TitleState{}
	for _, ts := range m.user(uid).titles {
		if ts.State == state {
			out = append(out, ts)
		}
	}
	slices.SortFunc(out, newestFirst)
	return out, nil
}

// AddUpdates implements Store.
func (m *Memory) AddUpdates(_ context.Context, uid string, updates []Update) error {
	ids := make([]string, len(updates))
	for i, u := range updates {
		id, err := updateID(u)
		if err != nil {
			return err
		}
		ids[i] = id
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.user(uid).updates
	for i, u := range updates {
		if _, ok := kept[ids[i]]; !ok {
			u.FoundAt, u.Seen = u.FoundAt.UTC(), false
			kept[ids[i]] = u
		}
	}
	return nil
}

// Updates implements Store.
func (m *Memory) Updates(_ context.Context, uid string) ([]Update, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := slices.Collect(maps.Values(m.user(uid).updates))
	slices.SortFunc(out, newestFound)
	return out[:min(len(out), updateLimit)], nil
}

// MarkUpdatesSeen implements Store.
func (m *Memory) MarkUpdatesSeen(_ context.Context, uid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, u := range m.user(uid).updates {
		u.Seen = true
		m.user(uid).updates[id] = u
	}
	return nil
}

// ReleaseFilters implements Store.
func (m *Memory) ReleaseFilters(_ context.Context, uid string) (ReleaseFilters, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f := m.user(uid).filters
	f.Qualities = slices.Clone(f.Qualities)
	return f, nil
}

// SaveReleaseFilters implements Store.
func (m *Memory) SaveReleaseFilters(_ context.Context, uid string, f ReleaseFilters) error {
	if err := CheckReleaseFilters(f); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	f.Qualities = slices.Clone(f.Qualities)
	m.user(uid).filters = f
	return nil
}

// CreateCustomList implements Store.
func (m *Memory) CreateCustomList(_ context.Context, uid, name string) (CustomList, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.user(uid).lists.create(name, m.opts.now().UTC())
}

// CustomLists implements Store.
func (m *Memory) CustomLists(_ context.Context, uid string) ([]CustomList, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.user(uid).lists.all(), nil
}

// RemoveCustomList implements Store.
func (m *Memory) RemoveCustomList(_ context.Context, uid, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	lists := m.user(uid).lists
	delete(m.shared, lists[id].ShareID)
	return lists.delete(id)
}

// ShareList implements Store.
func (m *Memory) ShareList(_ context.Context, uid, id string, on bool) (CustomList, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	lists := m.user(uid).lists
	l, ok := lists[id]
	if !ok {
		return CustomList{}, ErrNoList
	}
	switch {
	case on && l.ShareID == "":
		l.ShareID = newShareID()
		m.shared[l.ShareID] = sharedList{uid: uid, id: id}
	case !on && l.ShareID != "":
		delete(m.shared, l.ShareID)
		l.ShareID = ""
	}
	lists[id] = l
	l, _ = lists.get(id)
	return l, nil
}

// SharedList implements Store.
func (m *Memory) SharedList(_ context.Context, shareID string) (CustomList, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ref, ok := m.shared[shareID]
	if !ok {
		return CustomList{}, false, nil
	}
	l, ok := m.user(ref.uid).lists.get(ref.id)
	return l, ok, nil
}

// CustomList implements Store.
func (m *Memory) CustomList(_ context.Context, uid, id string) (CustomList, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.user(uid).lists.get(id)
	return l, ok, nil
}

// AddToList implements Store.
func (m *Memory) AddToList(_ context.Context, uid, id string, t ListTitle) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.user(uid).lists.add(id, t, m.opts.now().UTC())
	return err
}

// RemoveFromList implements Store.
func (m *Memory) RemoveFromList(_ context.Context, uid, id, kind string, tmdbID int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, err := m.user(uid).lists.remove(id, kind, tmdbID, m.opts.now().UTC())
	return err
}

// SaveReview implements Store.
func (m *Memory) SaveReview(_ context.Context, uid string, r Review) error {
	r, id, empty, err := checkReview(r)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if empty {
		delete(m.user(uid).reviews, id)
		return nil
	}
	r.UpdatedAt = m.opts.now().UTC()
	m.user(uid).reviews[id] = r
	return nil
}

// Review implements Store.
func (m *Memory) Review(_ context.Context, uid, kind string, tmdbID int) (Review, error) {
	id, err := titleID(kind, tmdbID)
	if err != nil {
		return Review{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.user(uid).reviews[id], nil
}
