package handlers

import (
	"net/http"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/auth"
	"github.com/lieranderl/moviestracker-app/internal/sources"
)

// Catalog serves the catalog: the home rows, lists, titles, people and
// search, from TMDB (and IMDb ratings). The local app (Server) and the cloud
// web app (internal/web) both serve it, each with its own users.
type Catalog struct {
	clients         func() *sources.Clients        // the TMDB/JacRed/IMDb clients in use
	userFromRequest func(*http.Request) *auth.User // the signed-in user; nil when signed out
	signIn          string                         // where signed-out visitors are sent
	catalogTimeout  time.Duration                  // bounds each TMDB lookup of a request
}

// NewCatalog serves the catalog with clients, to the users user names;
// signed-out visitors are sent to signIn.
func NewCatalog(clients func() *sources.Clients, user func(*http.Request) *auth.User, signIn string, timeout time.Duration) *Catalog {
	return &Catalog{clients: clients, userFromRequest: user, signIn: signIn, catalogTimeout: timeout}
}

// Register adds the catalog's routes to mux, its home page at home (none
// when home is empty: serve it with Home).
func (c *Catalog) Register(mux *http.ServeMux, home string) {
	if home != "" {
		mux.HandleFunc("GET "+home, c.handleMoviesPage)
	}
	mux.HandleFunc("GET /browse/{slug}", c.handleBrowsePage)
	mux.HandleFunc("GET /discover", c.handleDiscoverPage)
	mux.HandleFunc("GET /movie/{id}", c.handleMoviePage)
	mux.HandleFunc("GET /tv/{id}", c.handleTVPage)
	mux.HandleFunc("GET /person/{id}", c.handlePersonPage)
	mux.HandleFunc("GET /search", c.handleSearchPage)
	mux.HandleFunc("GET /api/movies/imdb-rating", c.handleMovieIMDbRating)
	mux.HandleFunc("GET /api/tv/{id}/season/{season}", c.handleSeason)
	mux.HandleFunc("GET /api/search", c.handleSearchAPI)
	mux.HandleFunc("GET /api/discover", c.handleDiscover)
	mux.HandleFunc("GET /api/torrents", c.handleTorrentSearch)
	mux.HandleFunc("GET /api/browse/{slug}", c.handleBrowseMore)
	mux.HandleFunc("GET /api/discover/page", c.handleDiscoverMore)
}

// Home is the catalog's home page: trending titles and the discovery rows.
func (c *Catalog) Home(w http.ResponseWriter, r *http.Request) { c.handleMoviesPage(w, r) }
