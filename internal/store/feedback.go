package store

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"
	"time"
)

// Feedback is what a user told recommendations about a title.
type Feedback string

const (
	// NotInterested keeps a title out of the user's recommendations.
	NotInterested Feedback = "notInterested"
	// MoreLikeThis makes a title one recommendations start from.
	MoreLikeThis Feedback = "moreLikeThis"
)

// TitleRef is a title feedback was given on, and when.
type TitleRef struct {
	Kind   string // "movie" or "tv"
	TMDBID int
	Title  string
	At     time.Time
}

// FeedbackStore keeps each user's recommendation feedback. The local app
// keeps it in a file (OpenFeedbackFile), by account name.
type FeedbackStore interface {
	// SetFeedback gives the title the feedback (on), stamped now, or takes
	// it back (off).
	SetFeedback(ctx context.Context, uid string, f Feedback, ref TitleRef, on bool) error
	// FeedbackOn are the titles with the feedback, most recent first.
	FeedbackOn(ctx context.Context, uid string, f Feedback) ([]TitleRef, error)
	// ResetFeedback forgets all the user's feedback.
	ResetFeedback(ctx context.Context, uid string) error
}

// feedbackKey is a piece of feedback's key ("notInterested-movie-438631"),
// or why it cannot be kept.
func feedbackKey(f Feedback, ref TitleRef) (string, error) {
	if f != NotInterested && f != MoreLikeThis {
		return "", fmt.Errorf("feedback %q is unknown", f)
	}
	id, err := titleID(ref.Kind, ref.TMDBID)
	if err != nil {
		return "", err
	}
	return string(f) + "-" + id, nil
}

// feedbackEntry is a kept piece of feedback.
type feedbackEntry struct {
	Feedback Feedback
	TitleRef
}

// feedbackOn are the entries with f, most recent first.
func feedbackOn(entries map[string]feedbackEntry, f Feedback) []TitleRef {
	var out []TitleRef
	for _, e := range entries {
		if e.Feedback == f {
			out = append(out, e.TitleRef)
		}
	}
	slices.SortFunc(out, func(a, b TitleRef) int {
		return cmp.Or(b.At.Compare(a.At), cmp.Compare(b.TMDBID, a.TMDBID))
	})
	return out
}

// FeedbackFile is a FeedbackStore in one owner-only JSON file, replaced
// atomically on every change.
type FeedbackFile struct {
	opts  options
	path  string
	mu    sync.Mutex
	users map[string]map[string]feedbackEntry // by user, then by feedbackKey
}

// OpenFeedbackFile reads the feedback kept at path; a missing file is none.
func OpenFeedbackFile(path string, opts ...Option) (*FeedbackFile, error) {
	f := &FeedbackFile{opts: newOptions(opts), path: path, users: map[string]map[string]feedbackEntry{}}
	if err := readJSONFile(path, &f.users); err != nil {
		return nil, fmt.Errorf("read recommendation feedback %s: %w", path, err)
	}
	return f, nil
}

// SetFeedback implements FeedbackStore.
func (f *FeedbackFile) SetFeedback(_ context.Context, uid string, fb Feedback, ref TitleRef, on bool) error {
	key, err := feedbackKey(fb, ref)
	if err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.users[uid] == nil {
		f.users[uid] = map[string]feedbackEntry{}
	}
	if on {
		ref.At = f.opts.now().UTC()
		f.users[uid][key] = feedbackEntry{fb, ref}
	} else {
		delete(f.users[uid], key)
	}
	return f.save()
}

// FeedbackOn implements FeedbackStore.
func (f *FeedbackFile) FeedbackOn(_ context.Context, uid string, fb Feedback) ([]TitleRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return feedbackOn(f.users[uid], fb), nil
}

// ResetFeedback implements FeedbackStore.
func (f *FeedbackFile) ResetFeedback(_ context.Context, uid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.users, uid)
	return f.save()
}

func (f *FeedbackFile) save() error {
	if err := writeJSONFile(f.path, f.users); err != nil {
		return fmt.Errorf("save recommendation feedback: %w", err)
	}
	return nil
}
