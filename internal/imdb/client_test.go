package imdb

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClient_GetRating_Success(t *testing.T) {
	var requestCount int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requestCount, 1)
		if r.URL.Path != "/getimdb" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if id := r.URL.Query().Get("imdb_id"); id != "tt27165187" {
			t.Errorf("unexpected imdb_id: %s", id)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Rating{
			ID:     "tt27165187",
			Rating: "6.3",
			Votes:  "56844",
		})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, WithHTTPClient(ts.Client()), WithCacheTTL(5*time.Minute))

	ctx := context.Background()
	rating, err := client.GetRating(ctx, "tt27165187")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rating == nil {
		t.Fatal("expected rating to not be nil")
	}
	if rating.ID != "tt27165187" || rating.Rating != "6.3" || rating.Votes != "56844" {
		t.Errorf("unexpected rating values: %+v", rating)
	}
	if rating.FormattedVotes() != "57k" {
		t.Errorf("expected FormattedVotes() = 57k, got %s", rating.FormattedVotes())
	}
	if rating.Title() != "IMDb: 6.3/10 (57k votes)" {
		t.Errorf("expected Title() = \"IMDb: 6.3/10 (57k votes)\", got %s", rating.Title())
	}

	// Verify caching: second call within TTL does not hit the server
	cachedRating, err := client.GetRating(ctx, "tt27165187")
	if err != nil {
		t.Fatalf("unexpected error on second call: %v", err)
	}
	if cachedRating.Rating != "6.3" {
		t.Errorf("expected cached rating 6.3, got %s", cachedRating.Rating)
	}
	if count := atomic.LoadInt32(&requestCount); count != 1 {
		t.Errorf("expected 1 server request due to caching, got %d", count)
	}
}

func TestThePublicRatingServiceIsCalledWithoutCredentials(t *testing.T) {
	var authorization string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"Id":"tt1375666","Rating":"8.8","Votes":"2600000"}`))
	}))
	defer ts.Close()

	rating, err := NewClient(ts.URL).GetRating(context.Background(), "tt1375666")
	if err != nil || rating.Rating != "8.8" {
		t.Fatalf("GetRating() = %+v, %v", rating, err)
	}
	if authorization != "" {
		t.Errorf("Authorization = %q, want none: the service is public", authorization)
	}
}

func TestClient_GetRating_InvalidID(t *testing.T) {
	client := NewClient("https://example.com")
	ctx := context.Background()

	_, err := client.GetRating(ctx, "")
	if err == nil {
		t.Error("expected error for empty imdb id")
	}

	_, err = client.GetRating(ctx, "not-a-valid-id-12345678901234567890")
	if err == nil {
		t.Error("expected error for malformed imdb id")
	}
}

func TestClient_GetRating_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, WithHTTPClient(ts.Client()))
	ctx := context.Background()

	_, err := client.GetRating(ctx, "tt27165187")
	if err == nil {
		t.Error("expected error on server error")
	}
}

func TestClient_GetRating_ContextCanceled(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, WithHTTPClient(ts.Client()))
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := client.GetRating(ctx, "tt27165187")
	if err == nil {
		t.Error("expected error on canceled context")
	}
}

func TestRating_FormattedVotes_And_Title(t *testing.T) {
	tests := []struct {
		name      string
		rating    Rating
		wantVotes string
		wantTitle string
	}{
		{
			name:      "thousands plain e.g. 2000",
			rating:    Rating{Rating: "8.0", Votes: "2000"},
			wantVotes: "2k",
			wantTitle: "IMDb: 8.0/10 (2k votes)",
		},
		{
			name:      "thousands with comma e.g. 2,000",
			rating:    Rating{Rating: "8.0", Votes: "2,000"},
			wantVotes: "2k",
			wantTitle: "IMDb: 8.0/10 (2k votes)",
		},
		{
			name:      "raw service format 56844",
			rating:    Rating{Rating: "6.3", Votes: "56844"},
			wantVotes: "57k",
			wantTitle: "IMDb: 6.3/10 (57k votes)",
		},
		{
			name:      "already formatted 120k",
			rating:    Rating{Rating: "7.8", Votes: "120k"},
			wantVotes: "120k",
			wantTitle: "IMDb: 7.8/10 (120k votes)",
		},
		{
			name:      "empty votes",
			rating:    Rating{Rating: "7.8", Votes: ""},
			wantVotes: "",
			wantTitle: "IMDb: 7.8/10",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.rating.FormattedVotes(); got != tt.wantVotes {
				t.Errorf("FormattedVotes() = %q, want %q", got, tt.wantVotes)
			}
			if got := tt.rating.Title(); got != tt.wantTitle {
				t.Errorf("Title() = %q, want %q", got, tt.wantTitle)
			}
		})
	}
}

// The rating service's address stays private: a failed call names the
// service, not where it runs, in logs and on the dashboard.
func TestAFailedRatingCallDoesNotRevealTheServiceAddress(t *testing.T) {
	down := httptest.NewServer(http.NotFoundHandler())
	address := down.URL
	down.Close()

	_, err := NewClient(address).GetRating(context.Background(), "tt27165187")
	if err == nil {
		t.Fatal("a call to a stopped service succeeded")
	}
	if host := strings.TrimPrefix(address, "http://"); strings.Contains(err.Error(), host) {
		t.Errorf("error %q reveals the service address", err)
	}
	if !strings.Contains(err.Error(), "imdb rating") {
		t.Errorf("error %q does not say what failed", err)
	}
}
