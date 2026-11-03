package web_test

import (
	firestorepb "cloud.google.com/go/firestore/apiv1/firestorepb"
	"context"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/web"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"html"
	"net"
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

func TestCompletionActionsKeepTheirFileTargetAndPreserveThePrivateNote(t *testing.T) {
	h, session, users := favorites(t)
	if err := users.SaveProgress(t.Context(), "1098765", store.Progress{Hash: "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111", Index: 2, Kind: "movie", TMDBID: 438631, Title: "Dune", Position: 70 * time.Second, Duration: 2 * time.Hour}); err != nil {
		t.Fatal(err)
	}
	if err := users.SaveReview(t.Context(), "1098765", store.Review{Kind: "movie", TMDBID: 438631, Title: "Dune", Rating: 6, Note: "See it in IMAX."}); err != nil {
		t.Fatal(err)
	}
	key := "c_aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111_2_1"
	res := getWith(t, h, "/api/player/completion?hash=aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111&index=2&nonce=1", session)
	body := html.UnescapeString(res.Body.String())
	for _, want := range []string{`id="title-review-` + key + `"`, `id="watch-state-` + key + `"`, "See it in IMAX.", "player=" + key} {
		if !strings.Contains(body, want) {
			t.Fatalf("offer lacks scoped action %q: %s", want, body)
		}
	}
	res = sendSignals(t, h, http.MethodPost, "/api/titles/review?type=movie&id=438631&player="+key, `{"reviewRating":8,"reviewNote":"See it in IMAX."}`, session)
	body = html.UnescapeString(res.Body.String())
	if res.Code != 200 || !strings.Contains(body, `id="title-review-`+key+`"`) || strings.Contains(body, `id="title-review"`) || !strings.Contains(body, `id="watch-state-`+key+`"`) {
		t.Fatalf("rating patched another file: %d %s", res.Code, body)
	}
	got, err := users.Review(t.Context(), "1098765", "movie", 438631)
	if err != nil || got.Rating != 8 || got.Note != "See it in IMAX." {
		t.Fatalf("saved review = %+v, %v", got, err)
	}
}

type completionWriteFailure struct{ selectorReadFailure }

func (*completionWriteFailure) BeginTransaction(context.Context, *firestorepb.BeginTransactionRequest) (*firestorepb.BeginTransactionResponse, error) {
	return nil, status.Error(codes.PermissionDenied, "disposable storage unavailable")
}

func TestCompletionActionsExplainFailedWritesAndOfferAnExplicitRetry(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	firestorepb.RegisterFirestoreServer(server, &completionWriteFailure{})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	t.Setenv("FIRESTORE_EMULATOR_HOST", listener.Addr().String())
	users, err := store.NewFirestore(t.Context(), "completion-test", "moviestracker")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })
	g := newGoogle(t)
	cfg := withTMDB(t, g.config())
	session := signIn(t, web.New(cfg))
	cfg.Store = users
	h := web.New(cfg)
	key := "c_aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111_2_1"
	for _, action := range []struct{ path, body, target string }{
		{"/api/titles/review?type=movie&id=438631&player=" + key, `{"reviewRating":8,"reviewNote":"Keep my note"}`, "title-review-" + key},
		{"/api/titles/state?type=movie&id=438631&state=watched&player=" + key, `{}`, "watch-state-" + key},
	} {
		res := sendSignals(t, h, http.MethodPost, action.path, action.body, session)
		body := html.UnescapeString(res.Body.String())
		if res.Code != 200 || !strings.Contains(body, `id="`+action.target+`"`) || !strings.Contains(body, `role="alert"`) || !strings.Contains(body, "Retry") || !strings.Contains(body, "@post(") {
			t.Fatalf("failed action lacks explicit retry: %d %s", res.Code, body)
		}
	}
}

func TestCompletionActionScopesCannotRedirectPatchesToArbitraryTargets(t *testing.T) {
	h, session, _ := favorites(t)
	for _, key := range []string{"other-target", "c_aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111_0_1", "c_aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111_2_1.foo"} {
		res := sendSignals(t, h, http.MethodPost, "/api/titles/review?type=movie&id=438631&player="+key, `{"reviewRating":8,"reviewNote":""}`, session)
		if res.Code != 400 {
			t.Fatalf("invalid scope %q = %d", key, res.Code)
		}
	}
}

type completionReadFailure struct{ selectorReadFailure }

func (*completionReadFailure) BatchGetDocuments(req *firestorepb.BatchGetDocumentsRequest, stream firestorepb.Firestore_BatchGetDocumentsServer) error {
	for _, doc := range req.Documents {
		if strings.Contains(doc, "/progress/") {
			return status.Error(codes.PermissionDenied, "disposable progress unavailable")
		}
	}
	return (&selectorReadFailure{}).BatchGetDocuments(req, stream)
}

func TestFailedCompletionReadsNeverOfferActionsForAnUnknownMovie(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	firestorepb.RegisterFirestoreServer(server, &completionReadFailure{})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	t.Setenv("FIRESTORE_EMULATOR_HOST", listener.Addr().String())
	users, err := store.NewFirestore(t.Context(), "completion-read-test", "moviestracker")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Close() })
	g := newGoogle(t)
	cfg := g.config()
	session := signIn(t, web.New(cfg))
	cfg.Store = users
	res := getWith(t, web.New(cfg), "/api/player/completion?hash=aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111&index=2&nonce=1", session)
	if res.Code != 503 || strings.Contains(res.Body.String(), "@post(") || res.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("failed read offered an action: %d %s", res.Code, res.Body.String())
	}
}
