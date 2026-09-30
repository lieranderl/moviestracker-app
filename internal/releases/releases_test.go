package releases_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/firestore"

	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/releases"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

var found = time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)

// latest are 25 releases of the latest feed, release n found n minutes
// after found, so release 25 is the newest.
func latest() []releases.Release {
	var out []releases.Release
	for n := 1; n <= 25; n++ {
		out = append(out, releases.Release{
			ID: 1000 + n, Title: fmt.Sprintf("Фильм %d", n), OriginalTitle: fmt.Sprintf("Film %d", n),
			PosterPath: fmt.Sprintf("/p%d.jpg", n), ReleaseDate: "2026-09-01", VoteAverage: 7.5, VoteCount: 1200,
			FoundAt: found.Add(time.Duration(n) * time.Minute),
		})
	}
	return out
}

// sources are the implementations every behavior is checked against, each
// holding feeds: in memory, and in Firestore, as the backend writes it, when
// its emulator runs (FIRESTORE_EMULATOR_HOST).
func sources(t *testing.T, feeds map[releases.Feed][]releases.Release) map[string]releases.Source {
	t.Helper()
	out := map[string]releases.Source{"memory": releases.NewMemory(feeds)}
	if os.Getenv("FIRESTORE_EMULATOR_HOST") == "" {
		t.Log("Firestore not checked: set FIRESTORE_EMULATOR_HOST (make firestore)")
		return out
	}
	ctx := context.Background()
	database := "releases-" + strings.ToLower(rand.Text()[:12]) // one database per test: the feeds' names are fixed
	client, err := firestore.NewClientWithDatabase(ctx, "demo-moviestracker", database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	for feed, rs := range feeds {
		for _, r := range rs {
			// The fields and types the backend writes (moviestracker-backend,
			// internal/catalog/model.go).
			_, err := client.Collection(string(feed)).Doc(strconv.Itoa(r.ID)).Set(ctx, map[string]any{
				"id": strconv.Itoa(r.ID), "title": r.Title, "original_title": r.OriginalTitle,
				"poster_path": r.PosterPath, "backdrop_path": r.BackdropPath, "release_date": r.ReleaseDate,
				"vote_average": strconv.FormatFloat(r.VoteAverage, 'f', 1, 64), "vote_count": strconv.Itoa(r.VoteCount),
				"lasttimefound": r.FoundAt, "genre_ids": []int{18}, "Year": "2026",
			})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	fs, err := releases.NewFirestore(ctx, "demo-moviestracker", database)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fs.Close() })
	out["firestore"] = fs
	return out
}

func TestTheNewestReleasesComeFirstTwentyAPage(t *testing.T) {
	for name, src := range sources(t, map[releases.Feed][]releases.Release{releases.Latest: latest()}) {
		t.Run(name, func(t *testing.T) {
			ctx := i18n.WithLang(context.Background(), i18n.Russian)
			first, err := src.Page(ctx, releases.Latest, 1)
			if err != nil {
				t.Fatal(err)
			}
			if len(first.Items) != 20 || first.Items[0].ID != 1025 || first.Items[19].ID != 1006 || first.TotalPages != 2 {
				t.Fatalf("page 1 = %d items from %v, %d pages; want 20 from 1025 to 1006, of 2 pages", len(first.Items), ids(first.Items), first.TotalPages)
			}
			second, err := src.Page(ctx, releases.Latest, 2)
			if err != nil {
				t.Fatal(err)
			}
			if len(second.Items) != 5 || second.Items[0].ID != 1005 || second.Items[4].ID != 1001 {
				t.Errorf("page 2 = %v, want 1005 to 1001", ids(second.Items))
			}
			m := first.Items[0]
			if m.Title != "Фильм 25" || m.PosterPath != "/p25.jpg" || m.MediaType != "movie" || m.VoteAverage != 7.5 || m.VoteCount != 1200 || m.ReleaseDate != "2026-09-01" {
				t.Errorf("newest release = %+v, want Фильм 25 as the backend wrote it", m)
			}
		})
	}
}

func TestEnglishPagesShowTheOriginalTitle(t *testing.T) {
	for name, src := range sources(t, map[releases.Feed][]releases.Release{releases.HDR10: latest()[:1]}) {
		t.Run(name, func(t *testing.T) {
			p, err := src.Page(i18n.WithLang(context.Background(), i18n.English), releases.HDR10, 1)
			if err != nil || len(p.Items) != 1 || p.Items[0].Title != "Film 1" {
				t.Errorf("Page() = %+v, %v, want Film 1", p.Items, err)
			}
		})
	}
}

func TestAnEmptyFeedHasNoReleases(t *testing.T) {
	for name, src := range sources(t, nil) {
		t.Run(name, func(t *testing.T) {
			p, err := src.Page(context.Background(), releases.DolbyVision, 1)
			if err != nil || len(p.Items) != 0 {
				t.Errorf("Page() = %+v, %v, want no releases", p.Items, err)
			}
		})
	}
}

func ids(items []tmdb.MediaItem) []int {
	out := make([]int, 0, len(items))
	for _, m := range items {
		out = append(out, m.ID)
	}
	return out
}
