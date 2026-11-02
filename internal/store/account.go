package store

import (
	"context"
	"errors"
	"time"
)

// Account is everything kept for one user, as they can take it away.
type Account struct {
	Preferences    Preferences    `json:"preferences"`
	Favorites      []Favorite     `json:"favorites"`
	TorrServers    []TorrServer   `json:"torrServers"`
	Progress       []Progress     `json:"progress"`
	TitleStates    []TitleState   `json:"watchStates"`
	Updates        []Update       `json:"releaseAlerts"`
	ReleaseFilters ReleaseFilters `json:"releaseFilters"`
	Lists          []CustomList   `json:"lists"`
	Reviews        []Review       `json:"reviews"`
	// Episodes are the watched episodes, by series and then season.
	Episodes map[int]map[int][]int `json:"watchedEpisodes"`
	Follows  []FollowedSeries      `json:"followedSeries"`
	// MutedAlerts and AlertFeeds are the user's release alert choices.
	MutedAlerts []MutedTitle         `json:"mutedAlerts"`
	AlertFeeds  map[string]AlertFeed `json:"alertFeeds"`
	// Feedback is the recommendation feedback given, by kind.
	Feedback map[Feedback][]TitleRef `json:"recommendationFeedback"`
}

// AccountStore hands users everything kept for them, and deletes it.
type AccountStore interface {
	// ExportAccount is everything kept for the user.
	ExportAccount(ctx context.Context, uid string) (Account, error)
	// DeleteAccount deletes everything kept for the user, their shared
	// lists' links included. Deleting what is gone already is no error,
	// so a failed deletion can be tried again.
	DeleteAccount(ctx context.Context, uid string) error
	// AccountDeletedAt is when the user's account was last deleted, so
	// sessions signed in before can end; zero when it never was, or longer
	// ago than any session lasts (DeletedAccountKept).
	AccountDeletedAt(ctx context.Context, uid string) (time.Time, error)
	// AccountDeletionPending allows only deletion cleanup to resume after a partial failure.
	AccountDeletionPending(ctx context.Context, uid string) (bool, error)
}

// DeletedAccountKept is how long a deleted account's ID and deletion time
// are kept: as long as a session lasts.
const DeletedAccountKept = 30 * 24 * time.Hour

// withAlertChoices is a with the user's release alert choices.
func withAlertChoices(ctx context.Context, s UpdateStore, uid string, a Account) (Account, error) {
	var err error
	if a.MutedAlerts, err = s.MutedAlerts(ctx, uid); err != nil {
		return Account{}, err
	}
	if a.AlertFeeds, err = s.AlertFeeds(ctx, uid); err != nil {
		return Account{}, err
	}
	return a, nil
}

// ErrAccountDeleted refuses writes from a session issued before deletion.
var ErrAccountDeleted = errors.New("store: this account session has ended")

type accountSessionKey struct{}
type accountSession struct {
	uid    string
	issued time.Time
}

// WithAccountSession carries the authenticated session to atomic store writes.
func WithAccountSession(ctx context.Context, uid string, issued time.Time) context.Context {
	return context.WithValue(ctx, accountSessionKey{}, accountSession{uid: uid, issued: issued})
}
func accountSessionTime(ctx context.Context, uid string) (time.Time, bool) {
	s, ok := ctx.Value(accountSessionKey{}).(accountSession)
	return s.issued, ok && s.uid == uid
}

// deletionFenceTime rounds upward to Firestore's microsecond precision so a
// persisted cutoff never precedes the session it revokes.
func deletionFenceTime(at time.Time) time.Time {
	rounded := at.UTC().Truncate(time.Microsecond)
	if rounded.Before(at) {
		return rounded.Add(time.Microsecond)
	}
	return rounded
}

type deletionResumeKey struct{}

// ResumeAccountDeletion permits finishing an existing deletion, never starting a
// new deletion if another request already completed it.
func ResumeAccountDeletion(ctx context.Context) context.Context {
	return context.WithValue(ctx, deletionResumeKey{}, true)
}
func onlyResumeDeletion(ctx context.Context) bool {
	on, _ := ctx.Value(deletionResumeKey{}).(bool)
	return on
}

// accountFeedback is the user's recommendation feedback, by kind, for their
// account's export.
func accountFeedback(ctx context.Context, s FeedbackStore, uid string) (map[Feedback][]TitleRef, error) {
	out := map[Feedback][]TitleRef{}
	for _, kind := range []Feedback{NotInterested, MoreLikeThis} {
		refs, err := s.FeedbackOn(ctx, uid, kind)
		if err != nil {
			return nil, err
		}
		if len(refs) > 0 {
			out[kind] = refs
		}
	}
	return out, nil
}
