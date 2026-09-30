package releases

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

// Firestore is a Source reading the backend's collections in a Firestore
// database (with FIRESTORE_EMULATOR_HOST set, that emulator's).
type Firestore struct {
	client *firestore.Client
}

// NewFirestore opens database in the Google Cloud project.
func NewFirestore(ctx context.Context, project, database string) (*Firestore, error) {
	client, err := firestore.NewClientWithDatabase(ctx, project, database)
	if err != nil {
		return nil, fmt.Errorf("open firestore %s/%s: %w", project, database, err)
	}
	return &Firestore{client: client}, nil
}

// Close releases the connection to Firestore.
func (f *Firestore) Close() error { return f.client.Close() }

// record is a release document as the backend writes it
// (moviestracker-backend, internal/catalog/model.go): numbers as strings.
type record struct {
	ID            string    `firestore:"id"`
	Title         string    `firestore:"title"`
	OriginalTitle string    `firestore:"original_title"`
	PosterPath    string    `firestore:"poster_path"`
	BackdropPath  string    `firestore:"backdrop_path"`
	ReleaseDate   string    `firestore:"release_date"`
	VoteAverage   string    `firestore:"vote_average"`
	VoteCount     string    `firestore:"vote_count"`
	LastTimeFound time.Time `firestore:"lasttimefound"`
}

// Page implements Source. It reads one release past the page to know
// whether another follows; the feed's size is not counted.
func (f *Firestore) Page(ctx context.Context, feed Feed, page int) (tmdb.Page, error) {
	if err := checkPage(feed, page); err != nil {
		return tmdb.Page{}, err
	}
	docs, err := f.client.Collection(string(feed)).
		OrderBy("lasttimefound", firestore.Desc).OrderBy(firestore.DocumentID, firestore.Desc).
		Offset((page - 1) * PageSize).Limit(PageSize + 1).
		Documents(ctx).GetAll()
	if err != nil {
		return tmdb.Page{}, fmt.Errorf("read %s page %d: %w", feed, page, err)
	}
	out := tmdb.Page{Page: page, TotalPages: page}
	if len(docs) > PageSize && page < maxPages {
		out.TotalPages, docs = page+1, docs[:PageSize]
	}
	for _, doc := range docs {
		var rec record
		if err := doc.DataTo(&rec); err != nil {
			return tmdb.Page{}, fmt.Errorf("read release %s: %w", doc.Ref.ID, err)
		}
		id, err := strconv.Atoi(doc.Ref.ID)
		if rec.ID != "" {
			id, err = strconv.Atoi(rec.ID)
		}
		if err != nil || id <= 0 {
			continue // not a TMDB title: nothing to link it to
		}
		votes, _ := strconv.ParseFloat(rec.VoteAverage, 64)
		count, _ := strconv.Atoi(rec.VoteCount)
		out.Items = append(out.Items, item(ctx, Release{
			ID: id, Title: rec.Title, OriginalTitle: rec.OriginalTitle, PosterPath: rec.PosterPath,
			BackdropPath: rec.BackdropPath, ReleaseDate: rec.ReleaseDate, VoteAverage: votes, VoteCount: count,
			FoundAt: rec.LastTimeFound,
		}))
	}
	return out, nil
}
