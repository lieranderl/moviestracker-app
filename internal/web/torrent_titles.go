package web

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/handlers"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

// Bounds of the torrent title matching: the torrent lists are the visitors'
// own, named as they like, so neither TMDB searches nor what is kept may
// grow with them.
const (
	titleMissRetry     = 24 * time.Hour // a name TMDB has no title for waits this long
	titleBatch         = 20             // names looked up per posted list at most
	maxTitleMatches    = 10_000         // matched names kept
	maxPendingTitleIDs = 10_000         // names whose next lookup is kept
)

// titleMatches are the titles matched to torrent names, shared by every
// visitor: a name and year of a kind is the same title for all, and keyed
// by name (not hash) no visitor's list can change where another's links.
// They are kept in memory: a restart only looks the names up again.
type titleMatches struct {
	mu    sync.Mutex
	pages map[string]string // by handlers.TitleKey: "/movie/438631"
}

func (m *titleMatches) Known(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.pages[key]
	return ok
}

func (m *titleMatches) Remember(key string, t torrserver.Torrent, id int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.pages) >= maxTitleMatches {
		clear(m.pages)
	}
	m.pages[key] = fmt.Sprintf("/%s/%d", t.Category, id)
}

// pagesOf returns where each of torrents links by hash: its matched title's
// page ("" when none).
func (m *titleMatches) pagesOf(torrents []torrserver.Torrent) func(hash string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	byHash := map[string]string{}
	for _, t := range torrents {
		if page := m.pages[handlers.TitleKey(t)]; page != "" {
			byHash[strings.ToLower(t.Hash)] = page
		}
	}
	return func(hash string) string { return byHash[strings.ToLower(hash)] }
}

// newTitleMatcher looks torrents' titles up with the web app's TMDB client,
// keeping them in matches.
func (a *app) newTitleMatcher(matches *titleMatches) *handlers.TitleMatcher {
	m := handlers.NewTitleMatcher(context.Background(), func() tmdb.DetailsProvider { return a.cfg.Sources.Details }, catalogTimeout, handlers.TitleKey, matches)
	m.Miss, m.Batch, m.Pending = titleMissRetry, titleBatch, maxPendingTitleIDs
	return m
}

// titlePages starts matching the titles of torrents not matched yet and
// returns where those matched already link, and whether some are still
// being matched (the browser then posts the list again until they are).
func (a *app) titlePages(torrents []torrserver.Torrent) (page func(hash string) string, matching bool) {
	matching = a.titleMatcher.Match(torrents)
	return a.titles.pagesOf(torrents), matching
}
