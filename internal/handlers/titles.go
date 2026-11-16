package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

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
// Moviestracker has no page for (see TitleMatcher).
func (s *Server) matchTitles(torrents []torrserver.Torrent) {
	s.titles.Match(torrents)
}

// savedTitles keeps the local app's matched titles in its saved state.
type savedTitles struct{ s *Server }

func (st savedTitles) Known(hash string) bool {
	_, ok := st.s.store.State().Titles[hash]
	return ok
}

func (st savedTitles) Remember(hash string, t torrserver.Torrent, id int) {
	st.s.rememberTitle(hash, t.Category, id)
}

// username is the signed-in account's name, by which its progress is kept.
func (s *Server) username(r *http.Request) (string, bool) {
	user := s.userFromRequest(r)
	if user == nil {
		return "", false
	}
	return user.Username, true
}

// progressTorrent is the torrent hash names in TorrServer's list, carrying
// the TMDB title it was added for (as the web app's torrents carry it).
func (s *Server) progressTorrent(ctx context.Context, hash string) (torrserver.Torrent, bool) {
	torrents, err := s.torrServer.Client().ListTorrents(ctx)
	if err != nil {
		slog.Warn("listing torrents for progress failed", "error", err)
		return torrserver.Torrent{}, false
	}
	i := slices.IndexFunc(torrents, func(t torrserver.Torrent) bool { return strings.EqualFold(t.Hash, hash) })
	if i < 0 {
		return torrserver.Torrent{}, false
	}
	t := torrents[i]
	if ref, ok := s.store.State().Titles[strings.ToLower(hash)]; ok {
		if kind, _ := t.TMDB(); kind == "" {
			t.Data = fmt.Sprintf(`{"tmdb":{"id":%d,"type":%q}}`, ref.ID, ref.Kind)
		}
	}
	return t, true
}
