package jacred_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/jacred"
)

func TestUserCanSearchWhenPrimaryJacRedIsUnavailable(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer primary.Close()
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(jackettJSON))
	}))
	defer backup.Close()
	searcher := jacred.NewFallback(
		jacred.NewClient(primary.URL, jacred.WithSearchAPI(), jacred.WithAPIKey("primary-only")),
		jacred.NewClient(backup.URL, jacred.WithJackettAPI()),
		5*time.Second,
	)
	results, err := searcher.Search(t.Context(), jacred.Query{OriginalTitle: "Inception", Year: 2010})
	if err != nil || len(results) != 1 || results[0].Seeders != 230 {
		t.Fatalf("results = %+v, err = %v", results, err)
	}
}

func TestBackupIsOnlyUsedAfterPrimaryFailure(t *testing.T) {
	for _, tc := range []struct {
		name         string
		status       int
		body         string
		transportErr error
		cancel       bool
		wantBackup   bool
		wantError    bool
	}{
		{name: "primary results", status: 200, body: `[{"tracker":"primary","url":"https://example.com/1","title":"Inception 2010","magnet":"magnet:?xt=urn:btih:AAAA","relased":2010}]`},
		{name: "empty results", status: 200, body: `[]`},
		{name: "unauthorized", status: 401, wantBackup: true},
		{name: "forbidden", status: 403, wantBackup: true},
		{name: "rate limited", status: 429, wantBackup: true},
		{name: "bad gateway", status: 502, wantBackup: true},
		{name: "invalid JSON", status: 200, body: `<html>outage</html>`, wantBackup: true},
		{name: "transport timeout", transportErr: context.DeadlineExceeded, wantBackup: true},
		{name: "connection failure", transportErr: io.ErrUnexpectedEOF, wantBackup: true},
		{name: "cancelled request", cancel: true, transportErr: context.Canceled, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backupCalled := false
			hc := &http.Client{Transport: fallbackTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Host == "primary.example" {
					if tc.transportErr != nil {
						return nil, tc.transportErr
					}
					return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body)), Header: http.Header{}}, nil
				}
				backupCalled = true
				if r.Header.Get("Authorization") != "" || r.URL.Query().Has("apikey") {
					t.Error("primary key sent to backup")
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(jackettJSON)), Header: http.Header{}}, nil
			})}
			primary := jacred.NewClient("https://primary.example", jacred.WithHTTPClient(hc), jacred.WithAPIKey("primary-only"))
			backup := jacred.NewClient("https://backup.example", jacred.WithHTTPClient(hc), jacred.WithJackettAPI())
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			results, err := jacred.NewFallback(primary, backup, time.Second).Search(ctx, jacred.Query{OriginalTitle: "Inception", Year: 2010})
			if (err != nil) != tc.wantError || backupCalled != tc.wantBackup {
				t.Fatalf("err = %v, backup called = %v", err, backupCalled)
			}
			if tc.wantBackup && (len(results) != 1 || results[0].Seeders != 230) {
				t.Fatalf("fallback results = %+v", results)
			}
			if tc.cancel && !errors.Is(err, context.Canceled) {
				t.Errorf("cancellation lost: %v", err)
			}
			if tc.name == "empty results" && len(results) != 0 {
				t.Errorf("empty search = %+v", results)
			}
			if tc.name == "primary results" && (len(results) != 1 || results[0].Tracker != "primary") {
				t.Errorf("primary results replaced: %+v", results)
			}
		})
	}
}

func TestPrimaryTimeoutLeavesTimeForBackup(t *testing.T) {
	hc := &http.Client{Transport: fallbackTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "primary.example" {
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		if r.Context().Err() != nil {
			t.Error("backup inherited expired primary context")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(jackettJSON)), Header: http.Header{}}, nil
	})}
	primary := jacred.NewClient("https://primary.example", jacred.WithHTTPClient(hc))
	backup := jacred.NewClient("https://backup.example", jacred.WithHTTPClient(hc), jacred.WithJackettAPI())
	results, err := jacred.NewFallback(primary, backup, 10*time.Millisecond).Search(t.Context(), jacred.Query{OriginalTitle: "Inception"})
	if err != nil || len(results) != 1 {
		t.Fatalf("results = %+v, err = %v", results, err)
	}
}

func TestUserGetsAnErrorWhenBothSourcesFail(t *testing.T) {
	hc := &http.Client{Transport: fallbackTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "primary.example" {
			return nil, io.ErrUnexpectedEOF
		}
		return &http.Response{StatusCode: 502, Body: io.NopCloser(strings.NewReader("outage")), Header: http.Header{}}, nil
	})}
	s := jacred.NewFallback(
		jacred.NewClient("https://primary.example", jacred.WithHTTPClient(hc)),
		jacred.NewClient("https://backup.example", jacred.WithHTTPClient(hc), jacred.WithJackettAPI()),
		time.Second,
	)
	_, err := s.Search(t.Context(), jacred.Query{OriginalTitle: "Inception"})
	if !errors.Is(err, io.ErrUnexpectedEOF) || !strings.Contains(err.Error(), "fallback JacRed: jacred returned status 502") {
		t.Fatalf("both failures should be reported: %v", err)
	}
}

type fallbackTransport func(*http.Request) (*http.Response, error)

func (f fallbackTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
