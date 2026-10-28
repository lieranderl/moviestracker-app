package sources_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/jacred"
	"github.com/lieranderl/moviestracker-app/internal/sources"
)

func TestAppAndWebCanOptIntoKeylessJacRedBackup(t *testing.T) {
	hc := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.jacred.su" {
			if r.Header.Get("Authorization") != "Bearer primary-key" {
				t.Error("primary key missing")
			}
			return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("outage")), Header: http.Header{}}, nil
		}
		if r.URL.Host != "jac.red" || r.URL.Path != "/api/v2.0/indexers/all/results" || r.Header.Get("Authorization") != "" || r.URL.Query().Has("apikey") {
			t.Errorf("unexpected fallback request: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"Results":[{"Tracker":"rutor","Details":"https://rutor.info/1","Title":"Inception 2010","MagnetUri":"magnet:?xt=urn:btih:AAAA","info":{"relased":2010}}]}`)), Header: http.Header{}}, nil
	})}
	connector := sources.Connector{JacRedHTTP: hc, JacRedFallbackURL: "https://jac.red"}                      // #nosec G101 -- a public service URL, not a credential
	clients := connector.Connect(config.Sources{JacRedURL: "https://jacred.su", JacRedAPIKey: "primary-key"}) // #nosec G101 -- test fixture
	results, err := clients.Torrents.Search(t.Context(), jacred.Query{OriginalTitle: "Inception", Year: 2010})
	if err != nil || len(results) != 1 || results[0].Tracker != "rutor" {
		t.Fatalf("results = %+v, err = %v", results, err)
	}
}

func TestPrivateJacRedFailureDoesNotSendSearchesToPublicBackup(t *testing.T) {
	hc := &http.Client{Transport: transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "private.example" {
			t.Errorf("private search sent to public backup: %s", r.URL.Host)
		}
		return &http.Response{StatusCode: 503, Body: io.NopCloser(strings.NewReader("outage")), Header: http.Header{}}, nil
	})}
	connector := sources.Connector{JacRedHTTP: hc, JacRedFallbackURL: "https://jac.red"}                            // #nosec G101 -- public service URL
	clients := connector.Connect(config.Sources{JacRedURL: "https://private.example", JacRedAPIKey: "private-key"}) // #nosec G101 -- test fixture
	_, err := clients.Torrents.Search(t.Context(), jacred.Query{OriginalTitle: "Inception", Year: 2010})
	if err == nil {
		t.Fatal("private source outage was hidden")
	}
}

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
