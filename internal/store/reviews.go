package store

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Review is a user's own rating of a title and their note on it, as TMDB
// names the title.
type Review struct {
	Kind      string // "movie" or "tv"
	TMDBID    int
	Title     string
	Poster    string // TMDB image path
	Rating    int    // 1 to 10; 0 is none
	Note      string // private to the user
	UpdatedAt time.Time
}

// ReviewStore keeps each user's ratings and notes. The local app keeps
// them in a file (OpenReviewFile), by account name.
type ReviewStore interface {
	// SaveReview keeps r, its note trimmed, stamped now; one with neither
	// a rating nor a note forgets the title's.
	SaveReview(ctx context.Context, uid string, r Review) error
	// Review is the user's review of a title; the zero Review when none.
	Review(ctx context.Context, uid, kind string, tmdbID int) (Review, error)
}

// maxNote is how many characters a note may have.
const maxNote = 2000

// ErrReview is why a rating or note cannot be kept.
var ErrReview = fmt.Errorf("a rating is 1 to 10 and a note has at most %d characters", maxNote)

// checkReview is r with its note trimmed and its key, or why it cannot be
// kept. empty says it is neither rated nor noted.
func checkReview(r Review) (out Review, id string, empty bool, err error) {
	r.Note = strings.TrimSpace(r.Note)
	if r.Rating < 0 || r.Rating > 10 || utf8.RuneCountInString(r.Note) > maxNote {
		return r, "", false, ErrReview
	}
	id, err = titleID(r.Kind, r.TMDBID)
	return r, id, r.Rating == 0 && r.Note == "", err
}
