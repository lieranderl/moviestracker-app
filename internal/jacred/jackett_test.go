package jacred_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/jacred"
)

const jackettJSON = `{"Results":[{"Tracker":"rutracker","Details":"https://rutracker.org/forum/viewtopic.php?t=5053009","Title":"Inception 2010 HDR 2160p","Size":47000000000,"PublishDate":"2025-11-26T15:54:00","Seeders":230,"Peers":10,"MagnetUri":"magnet:?xt=urn:btih:61190300e8e40e561790e33806f0db89d89ab24a","info":{"quality":2160,"videotype":"hdr","voices":["Dub"],"sizeName":"43.77 GB","relased":2010,"seasons":[2]}}]}`

func TestUserCanSearchJackettWithoutSendingACredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2.0/indexers/all/results" || r.URL.Query().Get("Query") != "Inception" {
			t.Errorf("unexpected search: %s", r.URL)
		}
		if r.Header.Get("Authorization") != "" || r.URL.Query().Has("apikey") {
			t.Error("Jackett request includes a credential")
		}
		if r.UserAgent() != "Mozilla/5.0" {
			t.Errorf("user-agent = %q", r.UserAgent())
		}
		_, _ = w.Write([]byte(jackettJSON))
	}))
	defer server.Close()
	client := jacred.NewClient(server.URL, jacred.WithJackettAPI(), jacred.WithAPIKey("primary-only"))
	results, err := client.Search(t.Context(), jacred.Query{OriginalTitle: "Inception", Year: 2010, Qualities: []int{2160}, HDR: true})
	if err != nil || len(results) != 1 {
		t.Fatalf("results = %+v, err = %v", results, err)
	}
	r := results[0]
	if r.Tracker != "rutracker" || r.Seeders != 230 || r.Peers != 10 || r.Year != 2010 || r.Size != 47000000000 || r.SizeName != "43.77 GB" || r.Quality != 2160 || !r.HDR || len(r.Voices) != 1 || r.Voices[0] != "Dub" || len(r.Seasons) != 1 || r.Seasons[0] != 2 {
		t.Fatalf("release metadata lost: %+v", r)
	}
	if !r.CreatedAt.Equal(time.Date(2025, 11, 26, 15, 54, 0, 0, time.UTC)) {
		t.Errorf("published = %v", r.CreatedAt)
	}
}

func TestJackettSeasonSearchExcludesReleasesWithoutMatchingSeasonMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Results":[
			{"Tracker":"rutor","Details":"https://rutor.info/1","Title":"Severance S01E01","MagnetUri":"magnet:?xt=urn:btih:AAAA","info":{}},
			{"Tracker":"rutor","Details":"https://rutor.info/2","Title":"Severance S01E02 2022","MagnetUri":"magnet:?xt=urn:btih:BBBB","info":{"relased":2022}},
			{"Tracker":"rutor","Details":"https://rutor.info/3","Title":"Severance S02E01","MagnetUri":"magnet:?xt=urn:btih:CCCC","info":{"seasons":[2]}},
			{"Tracker":"rutor","Details":"https://rutor.info/4","Title":"Severance S01E03","MagnetUri":"magnet:?xt=urn:btih:DDDD","info":{"seasons":[1]}}
		]}`))
	}))
	defer server.Close()
	client := jacred.NewClient(server.URL, jacred.WithJackettAPI())
	results, err := client.Search(t.Context(), jacred.Query{OriginalTitle: "Severance", Year: 2022, SeasonYear: 2025, Season: 2})
	if err != nil || len(results) != 1 || results[0].Title != "Severance S02E01" {
		t.Fatalf("season-two results = %+v, err = %v", results, err)
	}
}
