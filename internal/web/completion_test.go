package web_test

import (
	"github.com/lieranderl/moviestracker-app/internal/store"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestACompletionOfferUsesOnlyTheSignedInUsersKnownMovieAndDoesNotWrite(t *testing.T) {
	h, session, users := torrServers(t)
	if res := postPlayer(t, h, "/api/progress", duneProgress, session); res.Code != http.StatusNoContent {
		t.Fatal(res.Code)
	}
	before, _, _ := users.Progress(t.Context(), "1098765", "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", 2)
	res := getWith(t, h, "/api/player/completion?hash=aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111&index=2&nonce=1", session)
	if res.Code != 200 || !strings.Contains(res.Body.String(), "Finished watching?") || !strings.Contains(res.Body.String(), "/movie/438631#similar") {
		t.Fatalf("completion = %d %s", res.Code, res.Body.String())
	}
	after, _, _ := users.Progress(t.Context(), "1098765", before.Hash, 2)
	state, err := users.TitleState(t.Context(), "1098765", "movie", 438631)
	if err != nil || state.State != "" || after != before {
		t.Fatalf("offer wrote viewing data: %+v, %+v", state, after)
	}
	review, err := users.Review(t.Context(), "1098765", "movie", 438631)
	if err != nil || review.Rating != 0 || review.Note != "" {
		t.Fatalf("offer wrote a review: %+v", review)
	}
}

func TestCompletionOffersRejectUnknownSeriesAndOtherAccounts(t *testing.T) {
	for _, tc := range []struct {
		name, uid, kind string
		id              int
	}{
		{"another account", "other", "movie", 438631},
		{"unknown identity", "1098765", "", 0},
		{"series file", "1098765", "tv", 1399},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, session, users := torrServers(t)
			if err := users.SaveProgress(t.Context(), tc.uid, store.Progress{Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 2, Kind: tc.kind, TMDBID: tc.id, Title: "Dune", Position: 70 * time.Second, Duration: 2 * time.Hour}); err != nil {
				t.Fatal(err)
			}
			res := getWith(t, h, "/api/player/completion?hash=aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111&index=2&nonce=1", session)
			if res.Code != 204 || res.Body.Len() != 0 {
				t.Fatalf("suppressed offer = %d %s", res.Code, res.Body.String())
			}
		})
	}
	h, session, _ := torrServers(t)
	path := "/api/player/completion?hash=aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111&index=2&nonce=1"
	if res := getWith(t, h, path, httptest.NewRecorder()); res.Code != 401 {
		t.Fatal(res.Code)
	}
	for _, query := range []string{"hash=no&index=2&nonce=1", "hash=aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111&index=0&nonce=1", "hash=aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111&index=2&nonce=-1"} {
		if res := getWith(t, h, "/api/player/completion?"+query, session); res.Code != 400 {
			t.Fatal(res.Code)
		}
	}
}
