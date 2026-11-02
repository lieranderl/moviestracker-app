package store

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"sync"
)

// EpisodeStore keeps which episodes of each series a user watched. Marking
// episodes never marks the series itself watched. The local app keeps them
// in a file (OpenEpisodeFile), by account name.
type EpisodeStore interface {
	// WatchedEpisodes are the series' episodes the user marked watched, by
	// season, each season's in order; a season with none is left out.
	WatchedEpisodes(ctx context.Context, uid string, tvID int) (map[int][]int, error)
	// MarkEpisodes marks these episodes of the season watched, or not.
	MarkEpisodes(ctx context.Context, uid string, tvID, season int, episodes []int, watched bool) error
}

// checkEpisodes is why episodes of the season of series tvID cannot be
// marked, if they cannot.
func checkEpisodes(tvID, season int, episodes []int) error {
	if tvID <= 0 {
		return fmt.Errorf("series TMDB ID %d is not positive", tvID)
	}
	if season < 0 {
		return fmt.Errorf("season %d is negative", season)
	}
	for _, e := range episodes {
		if e <= 0 {
			return fmt.Errorf("episode %d is not positive", e)
		}
	}
	return nil
}

// markedEpisodes is a season's watched episodes after marking episodes
// watched or not: sorted, each once.
func markedEpisodes(kept, episodes []int, watched bool) []int {
	out := slices.Clone(kept)
	if watched {
		out = append(out, episodes...)
	} else {
		out = slices.DeleteFunc(out, func(e int) bool { return slices.Contains(episodes, e) })
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// seriesEpisodes are one series' watched episodes, by season (as text, the
// key JSON and Firestore maps take).
type seriesEpisodes map[string][]int

func (s seriesEpisodes) mark(season int, episodes []int, watched bool) {
	key := strconv.Itoa(season)
	if kept := markedEpisodes(s[key], episodes, watched); len(kept) > 0 {
		s[key] = kept
	} else {
		delete(s, key)
	}
}

func (s seriesEpisodes) bySeason() map[int][]int {
	out := map[int][]int{}
	for key, episodes := range s {
		if season, err := strconv.Atoi(key); err == nil && len(episodes) > 0 {
			out[season] = slices.Clone(episodes)
		}
	}
	return out
}

// EpisodeFile is an EpisodeStore in one owner-only JSON file, replaced
// atomically on every change.
type EpisodeFile struct {
	path  string
	mu    sync.Mutex
	users map[string]map[string]seriesEpisodes // by user, then by series ID
}

// OpenEpisodeFile reads the watched episodes kept at path; a missing file
// is none.
func OpenEpisodeFile(path string) (*EpisodeFile, error) {
	f := &EpisodeFile{path: path, users: map[string]map[string]seriesEpisodes{}}
	if err := readJSONFile(path, &f.users); err != nil {
		return nil, fmt.Errorf("read watched episodes %s: %w", path, err)
	}
	return f, nil
}

// WatchedEpisodes implements EpisodeStore.
func (f *EpisodeFile) WatchedEpisodes(_ context.Context, uid string, tvID int) (map[int][]int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.users[uid][strconv.Itoa(tvID)].bySeason(), nil
}

// MarkEpisodes implements EpisodeStore.
func (f *EpisodeFile) MarkEpisodes(_ context.Context, uid string, tvID, season int, episodes []int, watched bool) error {
	if err := checkEpisodes(tvID, season, episodes); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	series := f.users[uid]
	if series == nil {
		series = map[string]seriesEpisodes{}
		f.users[uid] = series
	}
	key := strconv.Itoa(tvID)
	if series[key] == nil {
		series[key] = seriesEpisodes{}
	}
	series[key].mark(season, episodes, watched)
	if len(series[key]) == 0 {
		delete(series, key)
	}
	if err := writeJSONFile(f.path, f.users); err != nil {
		return fmt.Errorf("save watched episodes: %w", err)
	}
	return nil
}
