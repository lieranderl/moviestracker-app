package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"cloud.google.com/go/firestore/apiv1/firestorepb"
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
	id, err := titleID(kind, tmdbID)
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
	id, err := titleID(fav.Kind, fav.TMDBID)
	if err != nil {
		return err
	}
	if fav.AddedAt.IsZero() {
		fav.AddedAt = f.opts.now()
	}
	fav.AddedAt = fav.AddedAt.UTC()
	err = f.changeLibrary(ctx, uid, func(tx *firestore.Transaction) error {
		return tx.Create(f.favorites(uid).Doc(id), favoriteRecord(fav))
	})
	if status.Code(err) == codes.AlreadyExists {
		return nil
	}
	if err != nil {
		return fmt.Errorf("add favourite: %w", err)
	}
	return nil
}

// changeLibrary makes a change to the user's library and gives the library
// a new stamp in one transaction, so a change is never kept without its
// stamp (which would leave summaries of the library stale).
func (f *Firestore) changeLibrary(ctx context.Context, uid string, change func(*firestore.Transaction) error) error {
	return f.client.RunTransaction(ctx, func(_ context.Context, tx *firestore.Transaction) error {
		if err := change(tx); err != nil {
			return err
		}
		return tx.Set(f.userDoc(uid), map[string]any{"libraryStamp": rand.Text()}, firestore.MergeAll)
	})
}

// LibraryStamp implements Store.
func (f *Firestore) LibraryStamp(ctx context.Context, uid string) (string, error) {
	snap, err := f.userDoc(uid).Get(ctx)
	if notFound(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read the library stamp: %w", err)
	}
	stamp, _ := snap.Data()["libraryStamp"].(string)
	return stamp, nil
}

// RemoveFavorite implements Store.
func (f *Firestore) RemoveFavorite(ctx context.Context, uid, kind string, tmdbID int) error {
	id, err := titleID(kind, tmdbID)
	if err != nil {
		return err
	}
	err = f.changeLibrary(ctx, uid, func(tx *firestore.Transaction) error {
		return tx.Delete(f.favorites(uid).Doc(id))
	})
	if err != nil {
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

// progressRecord is a users/{uid}/progress/{hash}-{file} document.
// Positions are whole seconds.
type progressRecord struct {
	Hash      string    `firestore:"hash"`
	Index     int       `firestore:"index"`
	File      string    `firestore:"file,omitempty"`
	Title     string    `firestore:"title"`
	Poster    string    `firestore:"poster,omitempty"`
	Kind      string    `firestore:"kind,omitempty"`
	TMDBID    int       `firestore:"tmdbId,omitempty"`
	Position  int64     `firestore:"position"`
	Duration  int64     `firestore:"duration"`
	UpdatedAt time.Time `firestore:"updatedAt"`
}

func (f *Firestore) progress(uid string) *firestore.CollectionRef {
	return f.userDoc(uid).Collection("progress")
}

// SaveProgress implements Store.
func (f *Firestore) SaveProgress(ctx context.Context, uid string, p Progress) error {
	id, err := progressID(p.Hash, p.Index)
	if err != nil {
		return err
	}
	rec := progressRecord{
		Hash: p.Hash, Index: p.Index, File: p.File, Title: p.Title, Poster: p.Poster, Kind: p.Kind, TMDBID: p.TMDBID,
		Position: int64(p.Position / time.Second), Duration: int64(p.Duration / time.Second), UpdatedAt: f.opts.now().UTC(),
	}
	if _, err := f.progress(uid).Doc(id).Set(ctx, rec); err != nil {
		return fmt.Errorf("save progress: %w", err)
	}
	return nil
}

// Progress implements Store.
func (f *Firestore) Progress(ctx context.Context, uid, hash string, index int) (Progress, bool, error) {
	id, err := progressID(hash, index)
	if err != nil {
		return Progress{}, false, err
	}
	snap, err := f.progress(uid).Doc(id).Get(ctx)
	if notFound(err) {
		return Progress{}, false, nil
	}
	if err != nil {
		return Progress{}, false, fmt.Errorf("read progress: %w", err)
	}
	p, err := readProgress(snap)
	return p, err == nil, err
}

// RecentProgress implements Store.
func (f *Firestore) RecentProgress(ctx context.Context, uid string, limit int) ([]Progress, error) {
	docs, err := f.progress(uid).OrderBy("updatedAt", firestore.Desc).Limit(recentWindow).Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("read progress: %w", err)
	}
	all := make([]Progress, 0, len(docs))
	for _, doc := range docs {
		p, err := readProgress(doc)
		if err != nil {
			return nil, err
		}
		all = append(all, p)
	}
	return unfinished(all, limit), nil
}

func readProgress(snap *firestore.DocumentSnapshot) (Progress, error) {
	var rec progressRecord
	if err := snap.DataTo(&rec); err != nil {
		return Progress{}, fmt.Errorf("read progress %s: %w", snap.Ref.ID, err)
	}
	return Progress{
		Hash: rec.Hash, Index: rec.Index, File: rec.File, Title: rec.Title, Poster: rec.Poster, Kind: rec.Kind, TMDBID: rec.TMDBID,
		Position: time.Duration(rec.Position) * time.Second, Duration: time.Duration(rec.Duration) * time.Second, UpdatedAt: rec.UpdatedAt,
	}, nil
}

// titleStateRecord is a users/{uid}/titles/{kind}-{id} document.
type titleStateRecord struct {
	Kind      string    `firestore:"kind"`
	TMDBID    int       `firestore:"tmdbId"`
	Title     string    `firestore:"title"`
	Poster    string    `firestore:"poster,omitempty"`
	State     string    `firestore:"state"`
	UpdatedAt time.Time `firestore:"updatedAt"`
}

func (f *Firestore) titles(uid string) *firestore.CollectionRef {
	return f.userDoc(uid).Collection("titles")
}

// SetTitleState implements Store.
func (f *Firestore) SetTitleState(ctx context.Context, uid string, ts TitleState) error {
	id, err := checkTitleState(ts)
	if err != nil {
		return err
	}
	if ts.State == "" {
		err := f.changeLibrary(ctx, uid, func(tx *firestore.Transaction) error {
			return tx.Delete(f.titles(uid).Doc(id))
		})
		if err != nil {
			return fmt.Errorf("clear watch state: %w", err)
		}
		return nil
	}
	ts.UpdatedAt = f.opts.now().UTC()
	err = f.changeLibrary(ctx, uid, func(tx *firestore.Transaction) error {
		return tx.Set(f.titles(uid).Doc(id), titleStateRecord(ts))
	})
	if err != nil {
		return fmt.Errorf("save watch state: %w", err)
	}
	return nil
}

// TitleState implements Store.
func (f *Firestore) TitleState(ctx context.Context, uid, kind string, tmdbID int) (TitleState, error) {
	id, err := titleID(kind, tmdbID)
	if err != nil {
		return TitleState{}, err
	}
	snap, err := f.titles(uid).Doc(id).Get(ctx)
	if notFound(err) {
		return TitleState{}, nil
	}
	if err != nil {
		return TitleState{}, fmt.Errorf("read watch state: %w", err)
	}
	var rec titleStateRecord
	if err := snap.DataTo(&rec); err != nil {
		return TitleState{}, fmt.Errorf("read watch state: %w", err)
	}
	return TitleState(rec), nil
}

// TitleStates implements Store. They are sorted here: ordering by updatedAt
// in a query filtered by state would need a composite index.
func (f *Firestore) TitleStates(ctx context.Context, uid, state string) ([]TitleState, error) {
	docs, err := f.titles(uid).Where("state", "==", state).Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("read watch states: %w", err)
	}
	out := make([]TitleState, 0, len(docs))
	for _, doc := range docs {
		var rec titleStateRecord
		if err := doc.DataTo(&rec); err != nil {
			return nil, fmt.Errorf("read watch state %s: %w", doc.Ref.ID, err)
		}
		out = append(out, TitleState(rec))
	}
	slices.SortFunc(out, newestFirst)
	return out, nil
}

// updateRecord is a users/{uid}/updates/{feed}-{id} document.
type updateRecord struct {
	Feed    string    `firestore:"feed"`
	TMDBID  int       `firestore:"tmdbId"`
	Title   string    `firestore:"title"`
	Poster  string    `firestore:"poster,omitempty"`
	FoundAt time.Time `firestore:"foundAt"`
	Seen    bool      `firestore:"seen"`
}

func (f *Firestore) updates(uid string) *firestore.CollectionRef {
	return f.userDoc(uid).Collection("updates")
}

// AddUpdates implements Store.
func (f *Firestore) AddUpdates(ctx context.Context, uid string, updates []Update) error {
	for _, u := range updates {
		id, err := updateID(u)
		if err != nil {
			return err
		}
		u.FoundAt, u.Seen = u.FoundAt.UTC(), false
		_, err = f.updates(uid).Doc(id).Create(ctx, updateRecord(u))
		if err != nil && status.Code(err) != codes.AlreadyExists {
			return fmt.Errorf("add update: %w", err)
		}
	}
	return nil
}

// Updates implements Store. Ordered by foundAt and then document ID, both
// descending, it needs only Firestore's single-field index on foundAt.
func (f *Firestore) Updates(ctx context.Context, uid, after string) ([]Update, string, error) {
	q := f.updates(uid).OrderBy("foundAt", firestore.Desc).OrderBy(firestore.DocumentID, firestore.Desc)
	if after != "" {
		at, id, err := parseUpdateCursor(after)
		if err != nil {
			return nil, "", err
		}
		q = q.StartAfter(at, id)
	}
	docs, err := q.Limit(updateLimit + 1).Documents(ctx).GetAll()
	if err != nil {
		return nil, "", fmt.Errorf("read updates: %w", err)
	}
	out := make([]Update, 0, len(docs))
	for _, doc := range docs {
		var rec updateRecord
		if err := doc.DataTo(&rec); err != nil {
			return nil, "", fmt.Errorf("read update %s: %w", doc.Ref.ID, err)
		}
		out = append(out, Update(rec))
	}
	if len(out) <= updateLimit {
		return out, "", nil
	}
	return out[:updateLimit], updateCursor(out[updateLimit-1]), nil
}

// UnseenUpdates implements Store, counting on the server.
func (f *Firestore) UnseenUpdates(ctx context.Context, uid string) (int, error) {
	unseen := f.updates(uid).Where("seen", "==", false)
	res, err := unseen.NewAggregationQuery().WithCount("n").Get(ctx)
	if err != nil {
		return 0, fmt.Errorf("count unseen updates: %w", err)
	}
	n, ok := res["n"].(*firestorepb.Value)
	if !ok {
		return 0, fmt.Errorf("count unseen updates: no count in %v", res)
	}
	return int(n.GetIntegerValue()), nil
}

// MarkUpdatesSeen implements Store.
func (f *Firestore) MarkUpdatesSeen(ctx context.Context, uid string, ids []string) error {
	refs := make([]*firestore.DocumentRef, len(ids))
	for i, id := range ids {
		refs[i] = f.updates(uid).Doc(id)
	}
	return f.markSeen(ctx, refs)
}

// MarkAllUpdatesSeen implements Store. It reads the unseen updates and
// leaves those found after through: filtering on foundAt in the query too
// would need a composite index.
func (f *Firestore) MarkAllUpdatesSeen(ctx context.Context, uid string, through time.Time) error {
	docs, err := f.updates(uid).Where("seen", "==", false).Documents(ctx).GetAll()
	if err != nil {
		return fmt.Errorf("read unseen updates: %w", err)
	}
	var refs []*firestore.DocumentRef
	for _, doc := range docs {
		var rec updateRecord
		if err := doc.DataTo(&rec); err != nil {
			return fmt.Errorf("read update %s: %w", doc.Ref.ID, err)
		}
		if !rec.FoundAt.After(through) {
			refs = append(refs, doc.Ref)
		}
	}
	return f.markSeen(ctx, refs)
}

// markSeen marks these update documents seen; ones that do not exist are
// left alone, so marking cannot create an update.
func (f *Firestore) markSeen(ctx context.Context, refs []*firestore.DocumentRef) error {
	bw := f.client.BulkWriter(ctx)
	jobs := make([]*firestore.BulkWriterJob, 0, len(refs))
	for _, ref := range refs {
		job, err := bw.Update(ref, []firestore.Update{{Path: "seen", Value: true}})
		if err != nil {
			bw.End()
			return fmt.Errorf("mark update seen: %w", err)
		}
		jobs = append(jobs, job)
	}
	bw.End()
	for _, job := range jobs {
		if _, err := job.Results(); err != nil && status.Code(err) != codes.NotFound {
			return fmt.Errorf("mark update seen: %w", err)
		}
	}
	return nil
}

// releaseFiltersRecord is the releaseFilters field of a users/{uid}
// document.
type releaseFiltersRecord struct {
	Sort           string `firestore:"sort,omitempty"`
	Qualities      []int  `firestore:"qualities,omitempty"`
	HDR            bool   `firestore:"hdr,omitempty"`
	HideRecordings bool   `firestore:"hideRecordings,omitempty"`
}

// ReleaseFilters implements Store.
func (f *Firestore) ReleaseFilters(ctx context.Context, uid string) (ReleaseFilters, error) {
	snap, err := f.userDoc(uid).Get(ctx)
	if notFound(err) {
		return ReleaseFilters{}, nil
	}
	if err != nil {
		return ReleaseFilters{}, fmt.Errorf("read release filters: %w", err)
	}
	var rec struct {
		Filters releaseFiltersRecord `firestore:"releaseFilters"`
	}
	if err := snap.DataTo(&rec); err != nil {
		return ReleaseFilters{}, fmt.Errorf("read release filters: %w", err)
	}
	return ReleaseFilters(rec.Filters), nil
}

// SaveReleaseFilters implements Store. Only the field is replaced, so the
// user's other preferences stay.
func (f *Firestore) SaveReleaseFilters(ctx context.Context, uid string, filters ReleaseFilters) error {
	if err := CheckReleaseFilters(filters); err != nil {
		return err
	}
	_, err := f.userDoc(uid).Set(ctx, map[string]any{"releaseFilters": releaseFiltersRecord(filters)}, firestore.Merge([]string{"releaseFilters"}))
	if err != nil {
		return fmt.Errorf("save release filters: %w", err)
	}
	return nil
}

func notFound(err error) bool {
	var se interface{ GRPCStatus() *status.Status }
	return errors.As(err, &se) && se.GRPCStatus().Code() == codes.NotFound
}

// customListRecord is a users/{uid}/lists/{id} document. Its titles are in
// it, newest first: a list holds few enough for one document.
type customListRecord struct {
	Name      string            `firestore:"name"`
	Titles    []listTitleRecord `firestore:"titles"`
	ShareID   string            `firestore:"shareId,omitempty"`
	UpdatedAt time.Time         `firestore:"updatedAt"`
}

// sharedListRecord is a sharedLists/{shareId} document: whose list its
// link opens.
type sharedListRecord struct {
	UID  string `firestore:"uid"`
	List string `firestore:"list"`
}

type listTitleRecord struct {
	Kind    string    `firestore:"kind"`
	TMDBID  int       `firestore:"tmdbId"`
	Title   string    `firestore:"title"`
	Poster  string    `firestore:"poster,omitempty"`
	AddedAt time.Time `firestore:"addedAt"`
}

func (f *Firestore) lists(uid string) *firestore.CollectionRef {
	return f.userDoc(uid).Collection("lists")
}

// list is list id's document; ok is false when id, as a URL gives it, is
// no document ID.
func (f *Firestore) list(uid, id string) (doc *firestore.DocumentRef, ok bool) {
	if id == "" || strings.ContainsAny(id, "/.") {
		return nil, false
	}
	return f.lists(uid).Doc(id), true
}

func toListRecord(l CustomList) customListRecord {
	rec := customListRecord{Name: l.Name, Titles: make([]listTitleRecord, len(l.Titles)), ShareID: l.ShareID, UpdatedAt: l.UpdatedAt}
	for i, t := range l.Titles {
		rec.Titles[i] = listTitleRecord(t)
	}
	return rec
}

func readCustomList(snap *firestore.DocumentSnapshot) (CustomList, error) {
	var rec customListRecord
	if err := snap.DataTo(&rec); err != nil {
		return CustomList{}, fmt.Errorf("read list %s: %w", snap.Ref.ID, err)
	}
	l := CustomList{ID: snap.Ref.ID, Name: rec.Name, Titles: make([]ListTitle, len(rec.Titles)), ShareID: rec.ShareID, UpdatedAt: rec.UpdatedAt}
	for i, t := range rec.Titles {
		l.Titles[i] = ListTitle(t)
	}
	return l, nil
}

// listCount is the users/{uid} field that counts the user's lists: the
// transactions that make and delete lists keep it, so lists made at once
// still stop at maxLists.
const listCount = "listCount"

// CreateCustomList implements Store.
func (f *Firestore) CreateCustomList(ctx context.Context, uid, name string) (CustomList, error) {
	name, err := checkListName(name)
	if err != nil {
		return CustomList{}, err
	}
	l := CustomList{ID: newID(), Name: name, Titles: []ListTitle{}, UpdatedAt: f.opts.now().UTC()}
	err = f.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		n, err := f.listCount(tx, uid)
		if err != nil {
			return err
		}
		if n >= maxLists {
			return ErrTooManyLists
		}
		if err := tx.Create(f.lists(uid).Doc(l.ID), toListRecord(l)); err != nil {
			return err
		}
		return tx.Set(f.userDoc(uid), map[string]any{listCount: n + 1}, firestore.Merge([]string{listCount}))
	})
	if errors.Is(err, ErrTooManyLists) {
		return CustomList{}, err
	}
	if err != nil {
		return CustomList{}, fmt.Errorf("make list: %w", err)
	}
	return l, nil
}

// listCount is how many lists the user has, read in tx.
func (f *Firestore) listCount(tx *firestore.Transaction, uid string) (int64, error) {
	snap, err := tx.Get(f.userDoc(uid))
	if notFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n, err := snap.DataAt(listCount)
	if err != nil {
		return 0, nil // no list made yet
	}
	count, _ := n.(int64)
	return count, nil
}

// CustomLists implements Store. They are sorted here, like watch states.
func (f *Firestore) CustomLists(ctx context.Context, uid string) ([]CustomList, error) {
	docs, err := f.lists(uid).Documents(ctx).GetAll()
	if err != nil {
		return nil, fmt.Errorf("read lists: %w", err)
	}
	out := make([]CustomList, 0, len(docs))
	for _, doc := range docs {
		l, err := readCustomList(doc)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	slices.SortFunc(out, recentlyChanged)
	return out, nil
}

// RemoveCustomList implements Store. Its link goes with it.
func (f *Firestore) RemoveCustomList(ctx context.Context, uid, id string) error {
	return f.changeList(ctx, uid, id, func(tx *firestore.Transaction, doc *firestore.DocumentRef, l CustomList) error {
		n, err := f.listCount(tx, uid)
		if err != nil {
			return err
		}
		if l.ShareID != "" {
			if err := tx.Delete(f.sharedLists().Doc(l.ShareID)); err != nil {
				return err
			}
		}
		if err := tx.Delete(doc); err != nil {
			return err
		}
		return tx.Set(f.userDoc(uid), map[string]any{listCount: max(n-1, 0)}, firestore.Merge([]string{listCount}))
	})
}

func (f *Firestore) sharedLists() *firestore.CollectionRef { return f.client.Collection("sharedLists") }

// ShareList implements Store.
func (f *Firestore) ShareList(ctx context.Context, uid, id string, on bool) (CustomList, error) {
	var out CustomList
	err := f.changeList(ctx, uid, id, func(tx *firestore.Transaction, doc *firestore.DocumentRef, l CustomList) error {
		out = l
		switch {
		case on && l.ShareID == "":
			out.ShareID = newShareID()
			if err := tx.Create(f.sharedLists().Doc(out.ShareID), sharedListRecord{UID: uid, List: id}); err != nil {
				return err
			}
		case !on && l.ShareID != "":
			out.ShareID = ""
			if err := tx.Delete(f.sharedLists().Doc(l.ShareID)); err != nil {
				return err
			}
		default:
			return nil
		}
		return tx.Set(doc, toListRecord(out))
	})
	return out, err
}

// SharedList implements Store.
func (f *Firestore) SharedList(ctx context.Context, shareID string) (CustomList, bool, error) {
	if shareID == "" || strings.ContainsAny(shareID, "/.") {
		return CustomList{}, false, nil
	}
	snap, err := f.sharedLists().Doc(shareID).Get(ctx)
	if notFound(err) {
		return CustomList{}, false, nil
	}
	if err != nil {
		return CustomList{}, false, fmt.Errorf("read shared list: %w", err)
	}
	var ref sharedListRecord
	if err := snap.DataTo(&ref); err != nil {
		return CustomList{}, false, fmt.Errorf("read shared list: %w", err)
	}
	l, ok, err := f.CustomList(ctx, ref.UID, ref.List)
	if err != nil || !ok || l.ShareID != shareID {
		return CustomList{}, false, err
	}
	return l, true, nil
}

// CustomList implements Store.
func (f *Firestore) CustomList(ctx context.Context, uid, id string) (CustomList, bool, error) {
	doc, ok := f.list(uid, id)
	if !ok {
		return CustomList{}, false, nil
	}
	snap, err := doc.Get(ctx)
	if notFound(err) {
		return CustomList{}, false, nil
	}
	if err != nil {
		return CustomList{}, false, fmt.Errorf("read list: %w", err)
	}
	l, err := readCustomList(snap)
	return l, err == nil, err
}

// changeList changes list id in a transaction, in which change writes.
func (f *Firestore) changeList(ctx context.Context, uid, id string, change func(*firestore.Transaction, *firestore.DocumentRef, CustomList) error) error {
	doc, ok := f.list(uid, id)
	if !ok {
		return ErrNoList
	}
	err := f.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		snap, err := tx.Get(doc)
		if notFound(err) {
			return ErrNoList
		}
		if err != nil {
			return err
		}
		l, err := readCustomList(snap)
		if err != nil {
			return err
		}
		return change(tx, doc, l)
	})
	if err != nil && !errors.Is(err, ErrNoList) && !errors.Is(err, ErrListFull) {
		return fmt.Errorf("change list: %w", err)
	}
	return err
}

// changeTitles changes list id's titles: change says whether it did.
func (f *Firestore) changeTitles(ctx context.Context, uid, id string, change func(CustomList) (CustomList, bool, error)) error {
	return f.changeList(ctx, uid, id, func(tx *firestore.Transaction, doc *firestore.DocumentRef, l CustomList) error {
		l, changed, err := change(l)
		if err != nil || !changed {
			return err
		}
		return tx.Set(doc, toListRecord(l))
	})
}

// AddToList implements Store.
func (f *Firestore) AddToList(ctx context.Context, uid, id string, t ListTitle) error {
	if _, err := titleID(t.Kind, t.TMDBID); err != nil {
		return err
	}
	at := f.opts.now().UTC()
	return f.changeTitles(ctx, uid, id, func(l CustomList) (CustomList, bool, error) { return withTitle(l, t, at) })
}

// RemoveFromList implements Store.
func (f *Firestore) RemoveFromList(ctx context.Context, uid, id, kind string, tmdbID int) error {
	at := f.opts.now().UTC()
	return f.changeTitles(ctx, uid, id, func(l CustomList) (CustomList, bool, error) {
		l, changed := withoutTitle(l, kind, tmdbID, at)
		return l, changed, nil
	})
}

// reviewRecord is a users/{uid}/reviews/{kind}-{id} document.
type reviewRecord struct {
	Kind      string    `firestore:"kind"`
	TMDBID    int       `firestore:"tmdbId"`
	Title     string    `firestore:"title"`
	Poster    string    `firestore:"poster,omitempty"`
	Rating    int       `firestore:"rating,omitempty"`
	Note      string    `firestore:"note,omitempty"`
	UpdatedAt time.Time `firestore:"updatedAt"`
}

func (f *Firestore) reviews(uid string) *firestore.CollectionRef {
	return f.userDoc(uid).Collection("reviews")
}

// SaveReview implements Store.
func (f *Firestore) SaveReview(ctx context.Context, uid string, r Review) error {
	r, id, empty, err := checkReview(r)
	if err != nil {
		return err
	}
	if empty {
		if _, err := f.reviews(uid).Doc(id).Delete(ctx); err != nil {
			return fmt.Errorf("clear review: %w", err)
		}
		return nil
	}
	r.UpdatedAt = f.opts.now().UTC()
	if _, err := f.reviews(uid).Doc(id).Set(ctx, reviewRecord(r)); err != nil {
		return fmt.Errorf("save review: %w", err)
	}
	return nil
}

// Review implements Store.
func (f *Firestore) Review(ctx context.Context, uid, kind string, tmdbID int) (Review, error) {
	id, err := titleID(kind, tmdbID)
	if err != nil {
		return Review{}, err
	}
	snap, err := f.reviews(uid).Doc(id).Get(ctx)
	if notFound(err) {
		return Review{}, nil
	}
	if err != nil {
		return Review{}, fmt.Errorf("read review: %w", err)
	}
	var rec reviewRecord
	if err := snap.DataTo(&rec); err != nil {
		return Review{}, fmt.Errorf("read review: %w", err)
	}
	return Review(rec), nil
}
