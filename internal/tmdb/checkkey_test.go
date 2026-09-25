package tmdb_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

func TestCheckKeyTellsAWorkingKeyFromARejectedOne(t *testing.T) {
	const v3Key = "0123456789abcdef0123456789abcdef"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/3/configuration" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("api_key") == v3Key || r.Header.Get("Authorization") == "Bearer good-token" {
			_, _ = w.Write([]byte(`{"images":{"secure_base_url":"https://image.tmdb.org/t/p/"}}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status_code":7,"status_message":"Invalid API key: You must be granted a valid key."}`))
	}))
	defer ts.Close()

	for _, key := range []string{v3Key, "good-token"} {
		if err := tmdb.NewClient(key, tmdb.WithBaseURL(ts.URL)).CheckKey(context.Background()); err != nil {
			t.Errorf("CheckKey(%q) = %v, want nil", key, err)
		}
	}
	if err := tmdb.NewClient("bad-token", tmdb.WithBaseURL(ts.URL)).CheckKey(context.Background()); !errors.Is(err, tmdb.ErrInvalidKey) {
		t.Errorf("CheckKey(bad) = %v, want ErrInvalidKey", err)
	}

	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer down.Close()
	err := tmdb.NewClient("good-token", tmdb.WithBaseURL(down.URL)).CheckKey(context.Background())
	if err == nil || errors.Is(err, tmdb.ErrInvalidKey) {
		t.Errorf("CheckKey(TMDB down) = %v, want an error that is not ErrInvalidKey", err)
	}
}
