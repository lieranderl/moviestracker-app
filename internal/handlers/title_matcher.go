package handlers

import (
	"cmp"
	"context"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

// titleRetry is how long a torrent whose TMDB lookup failed waits for the next.
const titleRetry = 10 * time.Minute

// titleYear is a torrent title as Moviestracker writes it: "Name (Year)".
var titleYear = regexp.MustCompile(`^(.+?) \((\d{4})\)$`)

// TitleMatches is where a TitleMatcher keeps what it found, by the key it
// was given for each torrent.
type TitleMatches interface {
	// Known says whether the torrent keyed key already has its title.
	Known(key string) bool
	// Remember records that t (keyed key) is the movie or series id of its
	// category.
	Remember(key string, t torrserver.Torrent, id int)
}

// TitleMatcher looks up, in the background, the titles of torrents
// Moviestracker has no page for: those named "Name (Year)" with a movie or
// tv category, as it names the releases it sends and as people often do.
// Only an exact name and year of that kind on TMDB counts. Each torrent is
// looked up once: again after a cooldown when TMDB failed, or when it
// found nothing and Miss is set, or when matches no longer knows it.
type TitleMatcher struct {
	details func() tmdb.DetailsProvider
	matches TitleMatches
	key     func(torrserver.Torrent) string
	ctx     context.Context // bounds the background lookups
	timeout time.Duration   // bounds each lookup

	// Miss is how long a torrent TMDB has no title for waits before it is
	// looked up again; 0 never looks it up again.
	Miss time.Duration
	// Batch is how many torrents one Match looks up at most (the others
	// wait for the next); 0 has no bound.
	Batch int
	// Pending is how many torrents' next lookups are kept at most; when
	// more are, the ones due are dropped, then all. 0 has no bound.
	Pending int

	mu       sync.Mutex
	next     map[string]time.Time // by key: no lookup before (after a miss or failure)
	inflight map[string]bool      // keys being looked up
	running  bool                 // a batch is being looked up
}

// NewTitleMatcher returns a matcher looking titles up with details, within
// ctx and timeout per lookup, keeping what it finds in matches by key.
func NewTitleMatcher(ctx context.Context, details func() tmdb.DetailsProvider, timeout time.Duration, key func(torrserver.Torrent) string, matches TitleMatches) *TitleMatcher {
	return &TitleMatcher{details: details, matches: matches, key: key, ctx: ctx, timeout: timeout, next: map[string]time.Time{}, inflight: map[string]bool{}}
}

// Match looks up, in the background, the titles of torrents not matched yet.
// While a batch is being looked up, another waits for the next Match. It
// says whether any of torrents is still to be looked up or being looked up:
// a later Match may then find its title.
func (m *TitleMatcher) Match(torrents []torrserver.Torrent) (pending bool) {
	if m.details() == nil {
		return false
	}
	now := time.Now()
	var todo []torrserver.Torrent
	m.mu.Lock()
	for _, t := range torrents {
		key := m.key(t)
		if m.inflight[key] {
			pending = true
			continue
		}
		if now.Before(m.next[key]) || !matchable(t) || m.matches.Known(key) {
			continue
		}
		pending = true
		if m.running || (m.Batch > 0 && len(todo) >= m.Batch) {
			continue
		}
		m.inflight[key] = true
		todo = append(todo, t)
	}
	m.prune(now)
	if len(todo) > 0 {
		m.running = true
	}
	m.mu.Unlock()
	if len(todo) == 0 {
		return pending
	}
	go func() {
		defer func() {
			m.mu.Lock()
			m.running = false
			m.mu.Unlock()
		}()
		for _, t := range todo {
			ctx, cancel := context.WithTimeout(m.ctx, m.timeout)
			id, err := findTitle(ctx, m.details(), t)
			cancel()
			key := m.key(t)
			if id > 0 && err == nil {
				m.matches.Remember(key, t, id)
			}
			m.mu.Lock()
			delete(m.inflight, key)
			switch {
			case err != nil:
				m.next[key] = time.Now().Add(titleRetry)
			case id == 0:
				m.next[key] = time.Now().Add(cmp.Or(m.Miss, 100*365*24*time.Hour)) // 0: never again
			}
			m.mu.Unlock()
		}
	}()
	return true
}

// prune keeps at most Pending next lookups: first dropping those due, then
// all (which only looks those torrents up again).
func (m *TitleMatcher) prune(now time.Time) {
	if m.Pending <= 0 || len(m.next) <= m.Pending {
		return
	}
	for key, at := range m.next {
		if !now.Before(at) {
			delete(m.next, key)
		}
	}
	if len(m.next) > m.Pending {
		clear(m.next)
	}
}

// matchable says whether t is named "Name (Year)" with a movie or tv category.
func matchable(t torrserver.Torrent) bool {
	return (t.Category == "movie" || t.Category == "tv") && titleYear.MatchString(strings.TrimSpace(t.Title))
}

// TitleKey is a torrent's title as matched: its category, name and year,
// the same for every torrent so named.
func TitleKey(t torrserver.Torrent) string {
	return t.Category + "\x00" + strings.ToLower(strings.TrimSpace(t.Title))
}

// findTitle returns the TMDB id of the movie or series t is named after (0
// when none matches exactly). Moviestracker names releases in the language
// of the page they were sent from, so each language is tried.
func findTitle(ctx context.Context, details tmdb.DetailsProvider, t torrserver.Torrent) (int, error) {
	m := titleYear.FindStringSubmatch(strings.TrimSpace(t.Title))
	if m == nil || details == nil {
		return 0, nil
	}
	for _, lang := range i18n.Supported {
		results, err := details.Search(i18n.WithLang(ctx, lang), m[1])
		if err != nil {
			return 0, err
		}
		items := results.Movies
		if t.Category == "tv" {
			items = results.Series
		}
		for _, item := range items {
			if strings.EqualFold(item.Title, m[1]) && item.ReleaseYear() == m[2] {
				return item.ID, nil
			}
		}
	}
	return 0, nil
}
