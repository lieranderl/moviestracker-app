// Package sources builds the clients of the external services discovery
// depends on (TMDB, JacRed, the IMDb rating service) from the saved settings,
// and checks new settings before they are saved.
package sources

import (
	"context"
	"fmt"
	"net/url"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/imdb"
	"github.com/lieranderl/moviestracker-app/internal/jacred"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
)

// Clients are the service clients for one set of sources. A nil field means
// that service is not configured.
type Clients struct {
	Catalog  tmdb.CatalogProvider
	Details  tmdb.DetailsProvider
	Torrents jacred.Searcher
	IMDb     imdb.RatingProvider
}

// Connector builds clients and checks sources. The zero value talks to the
// real services; tests point TMDBBaseURL at a fake.
type Connector struct {
	TMDBBaseURL string
	// Health, when set, records the outcome of every call the clients make.
	Health *Health
}

// Connect builds the clients for src.
func (c Connector) Connect(src config.Sources) Clients {
	var out Clients
	if src.TMDBKey != "" {
		client := watchedTMDB{c: c.tmdb(src.TMDBKey), h: c.Health}
		out.Catalog, out.Details = client, client
	}
	if src.JacRedURL != "" {
		out.Torrents = watchedJacRed{c: jacred.NewClient(src.JacRedURL, jacred.WithAPIKey(src.JacRedAPIKey)), h: c.Health}
	}
	if src.IMDbURL != "" && !src.IMDbOff {
		out.IMDb = watchedIMDb{c: imdb.NewClient(src.IMDbURL), h: c.Health}
	}
	return out
}

// CheckTMDB reports whether TMDB accepts key (tmdb.ErrInvalidKey if not).
func (c Connector) CheckTMDB(ctx context.Context, key string) error {
	return c.tmdb(key).CheckKey(ctx)
}

// probeQuery is a title every JacRed index carries, so a working instance
// always finds releases for it.
var probeQuery = jacred.Query{Title: "Начало", OriginalTitle: "Inception", Year: 2010}

// CheckJacRed runs a test search and returns how many releases it found.
func (c Connector) CheckJacRed(ctx context.Context, baseURL, apiKey string) (int, error) {
	if err := validHTTPURL(baseURL); err != nil {
		return 0, err
	}
	results, err := jacred.NewClient(baseURL, jacred.WithAPIKey(apiKey)).Search(ctx, probeQuery)
	if err != nil {
		return 0, err
	}
	return len(results), nil
}

func (c Connector) tmdb(key string) *tmdb.Client {
	var opts []tmdb.Option
	if c.TMDBBaseURL != "" {
		opts = append(opts, tmdb.WithBaseURL(c.TMDBBaseURL))
	}
	return tmdb.NewClient(key, opts...)
}

// validHTTPURL accepts an absolute http(s) URL with a host.
func validHTTPURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%q is not an http(s) address", raw)
	}
	return nil
}
