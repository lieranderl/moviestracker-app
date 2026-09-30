package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Firestore is a Store in a Firestore database. On Cloud Run it signs in as
// the service's account; with FIRESTORE_EMULATOR_HOST set it uses that
// emulator instead.
type Firestore struct {
	opts   options
	client *firestore.Client
}

// NewFirestore opens database in the Google Cloud project.
func NewFirestore(ctx context.Context, project, database string, opts ...Option) (*Firestore, error) {
	client, err := firestore.NewClientWithDatabase(ctx, project, database)
	if err != nil {
		return nil, fmt.Errorf("open firestore %s/%s: %w", project, database, err)
	}
	return &Firestore{opts: newOptions(opts), client: client}, nil
}

// Close releases the connection to Firestore.
func (f *Firestore) Close() error { return f.client.Close() }

func (f *Firestore) userDoc(uid string) *firestore.DocumentRef {
	return f.client.Collection("users").Doc(uid)
}

// userRecord is a users/{uid} document.
type userRecord struct {
	Language string `firestore:"language,omitempty"`
}

// Preferences implements Store.
func (f *Firestore) Preferences(ctx context.Context, uid string) (Preferences, error) {
	snap, err := f.userDoc(uid).Get(ctx)
	if notFound(err) {
		return Preferences{}, nil
	}
	if err != nil {
		return Preferences{}, fmt.Errorf("read preferences: %w", err)
	}
	var rec userRecord
	if err := snap.DataTo(&rec); err != nil {
		return Preferences{}, fmt.Errorf("read preferences: %w", err)
	}
	return Preferences(rec), nil
}

// SavePreferences implements Store.
func (f *Firestore) SavePreferences(ctx context.Context, uid string, p Preferences) error {
	_, err := f.userDoc(uid).Set(ctx, map[string]any{"language": p.Language}, firestore.MergeAll)
	if err != nil {
		return fmt.Errorf("save preferences: %w", err)
	}
	return nil
}

// favoriteRecord is a users/{uid}/favorites/{kind}-{id} document.
type favoriteRecord struct {
	Kind    string    `firestore:"kind"`
	TMDBID  int       `firestore:"tmdbId"`
	Title   string    `firestore:"title"`
	Poster  string    `firestore:"poster,omitempty"`
	AddedAt time.Time `firestore:"addedAt"`
}

func (f *Firestore) favorites(uid string) *firestore.CollectionRef {
	return f.userDoc(uid).Collection("favorites")
}

// Favorites implements Store.
func (f *Firestore) Favorites(ctx context.Context, uid string) ([]Favorite, error) {
	docs, err := f.favorites(uid).OrderBy("addedAt", firestore.Desc).Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("read favourites: %w", err)
	}
	favs := make([]Favorite, 0, len(docs))
	for _, doc := range docs {
		var rec favoriteRecord
		if err := doc.DataTo(&rec); err != nil {
			return nil, fmt.Errorf("read favourite %s: %w", doc.Ref.ID, err)
		}
		favs = append(favs, Favorite(rec))
	}
	return favs, nil
}

// IsFavorite implements Store.
func (f *Firestore) IsFavorite(ctx context.Context, uid, kind string, tmdbID int) (bool, error) {
	id, err := favoriteID(kind, tmdbID)
	if err != nil {
		return false, err
	}
	_, err = f.favorites(uid).Doc(id).Get(ctx)
	if notFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read favourite: %w", err)
	}
	return true, nil
}

// AddFavorite implements Store.
func (f *Firestore) AddFavorite(ctx context.Context, uid string, fav Favorite) error {
	id, err := favoriteID(fav.Kind, fav.TMDBID)
	if err != nil {
		return err
	}
	if fav.AddedAt.IsZero() {
		fav.AddedAt = f.opts.now()
	}
	fav.AddedAt = fav.AddedAt.UTC()
	_, err = f.favorites(uid).Doc(id).Create(ctx, favoriteRecord(fav))
	if err != nil && status.Code(err) != codes.AlreadyExists {
		return fmt.Errorf("add favourite: %w", err)
	}
	return nil
}

// RemoveFavorite implements Store.
func (f *Firestore) RemoveFavorite(ctx context.Context, uid, kind string, tmdbID int) error {
	id, err := favoriteID(kind, tmdbID)
	if err != nil {
		return err
	}
	if _, err := f.favorites(uid).Doc(id).Delete(ctx); err != nil {
		return fmt.Errorf("remove favourite: %w", err)
	}
	return nil
}

// torrServerRecord is a users/{uid}/torrservers/{id} document.
type torrServerRecord struct {
	Name    string    `firestore:"name"`
	URL     string    `firestore:"url"`
	AddedAt time.Time `firestore:"addedAt"`
}

func (f *Firestore) torrServers(uid string) *firestore.CollectionRef {
	return f.userDoc(uid).Collection("torrservers")
}

// TorrServers implements Store.
func (f *Firestore) TorrServers(ctx context.Context, uid string) ([]TorrServer, error) {
	docs, err := f.torrServers(uid).OrderBy("addedAt", firestore.Asc).Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("read TorrServers: %w", err)
	}
	out := make([]TorrServer, 0, len(docs))
	for _, doc := range docs {
		var rec torrServerRecord
		if err := doc.DataTo(&rec); err != nil {
			return nil, fmt.Errorf("read TorrServer %s: %w", doc.Ref.ID, err)
		}
		out = append(out, TorrServer{ID: doc.Ref.ID, Name: rec.Name, URL: rec.URL, AddedAt: rec.AddedAt})
	}
	return out, nil
}

// SaveTorrServer implements Store.
func (f *Firestore) SaveTorrServer(ctx context.Context, uid string, t TorrServer) (TorrServer, error) {
	t, err := checkTorrServer(t)
	if err != nil {
		return TorrServer{}, err
	}
	if t.ID != "" {
		doc := f.torrServers(uid).Doc(t.ID)
		_, err := doc.Update(ctx, []firestore.Update{{Path: "name", Value: t.Name}, {Path: "url", Value: t.URL}})
		if err == nil {
			snap, err := doc.Get(ctx)
			if err != nil {
				return TorrServer{}, fmt.Errorf("read TorrServer: %w", err)
			}
			var rec torrServerRecord
			if err := snap.DataTo(&rec); err != nil {
				return TorrServer{}, fmt.Errorf("read TorrServer: %w", err)
			}
			return TorrServer{ID: t.ID, Name: rec.Name, URL: rec.URL, AddedAt: rec.AddedAt}, nil
		}
		if !notFound(err) {
			return TorrServer{}, fmt.Errorf("save TorrServer: %w", err)
		}
	}
	if t.ID == "" {
		t.ID = newID()
	}
	t.AddedAt = f.opts.now().UTC()
	if _, err := f.torrServers(uid).Doc(t.ID).Set(ctx, torrServerRecord{Name: t.Name, URL: t.URL, AddedAt: t.AddedAt}); err != nil {
		return TorrServer{}, fmt.Errorf("save TorrServer: %w", err)
	}
	return t, nil
}

// RemoveTorrServer implements Store.
func (f *Firestore) RemoveTorrServer(ctx context.Context, uid, id string) error {
	if id == "" || strings.Contains(id, "/") {
		return nil
	}
	if _, err := f.torrServers(uid).Doc(id).Delete(ctx); err != nil {
		return fmt.Errorf("remove TorrServer: %w", err)
	}
	return nil
}

func notFound(err error) bool {
	var se interface{ GRPCStatus() *status.Status }
	return errors.As(err, &se) && se.GRPCStatus().Code() == codes.NotFound
}
