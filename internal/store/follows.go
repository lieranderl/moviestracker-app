package store

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"
)

// FollowedSeries is a series a user follows for its upcoming episodes.
type FollowedSeries struct {
	TMDBID int
	Title  string
	Poster string // TMDB image path
	At     time.Time
}

// FollowStore keeps the series each user follows. The local app keeps them
// in a file (OpenFollowFile), by account name.
type FollowStore interface {
	// Follow follows the series (on), stamped now, or stops following it.
	Follow(ctx context.Context, uid string, f FollowedSeries, on bool) error
	// Following says whether the user follows series tvID.
	Following(ctx context.Context, uid string, tvID int) (bool, error)
	// FollowedSeries are the series the user follows, most recently first.
	FollowedSeries(ctx context.Context, uid string) ([]FollowedSeries, error)
}

func checkFollow(f FollowedSeries) error {
	if f.TMDBID <= 0 {
		return fmt.Errorf("series TMDB ID %d is not positive", f.TMDBID)
	}
	return nil
}

func newestFollowed(a, b FollowedSeries) int {
	return cmp.Or(b.At.Compare(a.At), cmp.Compare(b.TMDBID, a.TMDBID))
}

// FollowFile is a FollowStore in one owner-only JSON file, replaced
// atomically on every change.
type FollowFile struct {
	opts  options
	path  string
	mu    sync.Mutex
	users map[string]map[string]FollowedSeries // by user, then by series ID
}

// OpenFollowFile reads the followed series kept at path; a missing file is
// none.
func OpenFollowFile(path string, opts ...Option) (*FollowFile, error) {
	f := &FollowFile{opts: newOptions(opts), path: path, users: map[string]map[string]FollowedSeries{}}
	if err := readJSONFile(path, &f.users); err != nil {
		return nil, fmt.Errorf("read followed series %s: %w", path, err)
	}
	return f, nil
}

// Follow implements FollowStore.
func (f *FollowFile) Follow(_ context.Context, uid string, s FollowedSeries, on bool) error {
	if err := checkFollow(s); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.users[uid] == nil {
		f.users[uid] = map[string]FollowedSeries{}
	}
	key := strconv.Itoa(s.TMDBID)
	if on {
		s.At = f.opts.now().UTC()
		f.users[uid][key] = s
	} else {
		delete(f.users[uid], key)
	}
	if err := writeJSONFile(f.path, f.users); err != nil {
		return fmt.Errorf("save followed series: %w", err)
	}
	return nil
}

// Following implements FollowStore.
func (f *FollowFile) Following(_ context.Context, uid string, tvID int) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.users[uid][strconv.Itoa(tvID)]
	return ok, nil
}

// FollowedSeries implements FollowStore.
func (f *FollowFile) FollowedSeries(_ context.Context, uid string) ([]FollowedSeries, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]FollowedSeries, 0, len(f.users[uid]))
	for _, s := range f.users[uid] {
		out = append(out, s)
	}
	slices.SortFunc(out, newestFollowed)
	return out, nil
}
