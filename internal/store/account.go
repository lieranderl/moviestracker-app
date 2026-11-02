package store

import (
	"context"
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
