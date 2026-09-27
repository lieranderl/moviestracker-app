package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

// titleRetry is how long a torrent whose TMDB lookup failed waits for the next.
const titleRetry = 10 * time.Minute

// titleLookups remembers which torrents' titles were looked up on TMDB, so
// each is looked up once (again after titleRetry when TMDB failed).
type titleLookups struct {
	mu   sync.Mutex
	next map[string]time.Time // by lowercase hash: no lookup before
}

// titleYear is a torrent title as Moviestracker writes it: "Name (Year)".
var titleYear = regexp.MustCompile(`^(.+?) \((\d{4})\)$`)

// rememberTitle records the movie or series a torrent belongs to.
func (s *Server) rememberTitle(hash, kind string, id int) {
	err := s.store.Update(func(st *config.State) error {
		if st.Titles == nil {
			st.Titles = map[string]config.TitleRef{}
		}
		st.Titles[strings.ToLower(hash)] = config.TitleRef{Kind: kind, ID: id}
		return nil
	})
	if err != nil {
		slog.Warn("could not save a torrent's title", "error", err)
	}
}

// forgetTitle drops a removed torrent's title.
func (s *Server) forgetTitle(hash string) {
	hash = strings.ToLower(hash)
	if _, ok := s.store.State().Titles[hash]; !ok {
		return
	}
	err := s.store.Update(func(st *config.State) error {
		delete(st.Titles, hash)
		return nil
	})
	if err != nil {
		slog.Warn("could not forget a removed torrent's title", "error", err)
	}
}

// titlePages returns where each torrent's card links: its movie or TV page.
func (s *Server) titlePages() func(hash string) string {
	titles := s.store.State().Titles
	return func(hash string) string {
		ref, ok := titles[strings.ToLower(hash)]
		if !ok || (ref.Kind != "movie" && ref.Kind != "tv") || ref.ID <= 0 {
			return ""
		}
		return fmt.Sprintf("/%s/%d", ref.Kind, ref.ID)
	}
}

// matchTitles looks up, in the background, the titles of listed torrents
// Moviestracker has no page for: those named "Name (Year)" with a movie or
// tv category, as it names the releases it sends and as people often do.
// Only an exact name and year of that kind on TMDB counts.
func (s *Server) matchTitles(torrents []torrserver.Torrent) {
	details := s.clients().Details
	if details == nil {
		return
	}
	known := s.store.State().Titles
	now := time.Now()
	var todo []torrserver.Torrent
	s.titles.mu.Lock()
	if s.titles.next == nil {
		s.titles.next = map[string]time.Time{}
	}
	for _, t := range torrents {
		hash := strings.ToLower(t.Hash)
		if _, ok := known[hash]; ok || now.Before(s.titles.next[hash]) {
			continue
		}
		if (t.Category != "movie" && t.Category != "tv") || !titleYear.MatchString(strings.TrimSpace(t.Title)) {
			continue
		}
		s.titles.next[hash] = now.Add(100 * 365 * 24 * time.Hour) // once, unless TMDB fails
		todo = append(todo, t)
	}
	s.titles.mu.Unlock()
	if len(todo) == 0 {
		return
	}
	go func() {
		for _, t := range todo {
			ctx, cancel := context.WithTimeout(s.streams, s.catalogTimeout)
			id, err := s.findTitle(ctx, t)
			cancel()
			switch {
			case err != nil:
				s.titles.mu.Lock()
				s.titles.next[strings.ToLower(t.Hash)] = time.Now().Add(titleRetry)
				s.titles.mu.Unlock()
			case id > 0:
				s.rememberTitle(t.Hash, t.Category, id)
			}
		}
	}()
}

// findTitle returns the TMDB id of the movie or series t is named after (0
// when none matches exactly). Moviestracker names releases in the language
// of the page they were sent from, so each language is tried.
func (s *Server) findTitle(ctx context.Context, t torrserver.Torrent) (int, error) {
	m := titleYear.FindStringSubmatch(strings.TrimSpace(t.Title))
	details := s.clients().Details
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
