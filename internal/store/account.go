package store

import "context"

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
}

// AccountStore hands users everything kept for them, and deletes it.
type AccountStore interface {
	// ExportAccount is everything kept for the user.
	ExportAccount(ctx context.Context, uid string) (Account, error)
	// DeleteAccount deletes everything kept for the user, their shared
	// lists' links included. Deleting what is gone already is no error,
	// so a failed deletion can be tried again.
	DeleteAccount(ctx context.Context, uid string) error
}
