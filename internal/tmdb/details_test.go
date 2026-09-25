package tmdb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeTMDB serves canned JSON bodies by URL path and counts requests.
func fakeTMDB(t *testing.T, routes map[string]string) (*Client, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		body, ok := routes[r.URL.Path]
		if !ok {
			http.Error(w, `{"status_code":34,"status_message":"The resource you requested could not be found."}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return NewClient("test-key", WithBaseURL(server.URL), WithHTTPClient(server.Client())), &hits
}

const movieJSON = `{
	"id": 27205, "title": "Inception", "original_title": "Inception",
	"tagline": "Your mind is the scene of the crime.",
	"overview": "Cobb steals secrets from dreams.",
	"poster_path": "/poster.jpg", "backdrop_path": "/backdrop.jpg",
	"release_date": "2010-07-15", "runtime": 148, "status": "Released",
	"vote_average": 8.4, "vote_count": 37000, "imdb_id": "tt1375666",
	"homepage": "https://www.warnerbros.com/movies/inception",
	"external_ids": {"imdb_id": "tt1375666", "wikidata_id": "Q25188", "facebook_id": "inception", "instagram_id": "inceptionmovie", "twitter_id": null},
	"genres": [{"id": 28, "name": "Action"}, {"id": 878, "name": "Science Fiction"}],
	"credits": {
		"cast": [
			{"id": 6193, "name": "Leonardo DiCaprio", "character": "Cobb", "profile_path": "/leo.jpg", "order": 0},
			{"id": 24045, "name": "Joseph Gordon-Levitt", "character": "Arthur", "profile_path": null, "order": 1}
		],
		"crew": [
			{"id": 525, "name": "Christopher Nolan", "job": "Director", "department": "Directing"},
			{"id": 525, "name": "Christopher Nolan", "job": "Screenplay", "department": "Writing"},
			{"id": 947, "name": "Hans Zimmer", "job": "Original Music Composer", "department": "Sound"}
		]
	},
	"videos": {"results": [
		{"key": "YoHD9XEInc0", "name": "Official Trailer", "site": "YouTube", "type": "Trailer", "official": true, "iso_639_1": "en"},
		{"key": "vimeo-key", "name": "Elsewhere", "site": "Vimeo", "type": "Trailer"}
	]},
	"images": {"logos": [{"file_path": "/logo.png", "iso_639_1": "en"}]},
	"release_dates": {"results": [
		{"iso_3166_1": "FR", "release_dates": [{"certification": "TP", "type": 3}]},
		{"iso_3166_1": "US", "release_dates": [{"certification": "", "type": 1}, {"certification": "PG-13", "type": 3}]}
	]},
	"recommendations": {"results": [{"id": 157336, "title": "Interstellar", "poster_path": "/interstellar.jpg", "release_date": "2014-11-05", "vote_average": 8.4, "media_type": "movie"}]},
	"similar": {"results": [{"id": 1124, "title": "The Prestige", "poster_path": "/prestige.jpg", "release_date": "2006-10-17"}]}
}`

func TestMovieDetailsIncludeCreditsVideosAndRecommendationsInOneRequest(t *testing.T) {
	client, hits := fakeTMDB(t, map[string]string{"/3/movie/27205": movieJSON})

	m, err := client.Movie(context.Background(), 27205)
	if err != nil {
		t.Fatalf("Movie(): %v", err)
	}
	if hits.Load() != 1 {
		t.Errorf("requests = %d, want 1", hits.Load())
	}
	if m.Title != "Inception" || m.ReleaseYear() != "2010" || m.MediaType != "movie" || m.ImdbID != "tt1375666" {
		t.Errorf("basic fields = %+v", m.MediaItem)
	}
	if m.Tagline != "Your mind is the scene of the crime." {
		t.Errorf("Tagline = %q", m.Tagline)
	}
	if got := m.FormattedRuntime(); got != "2h 28m" {
		t.Errorf("FormattedRuntime() = %q, want 2h 28m", got)
	}
	if len(m.Genres) != 2 || m.Genres[1].Name != "Science Fiction" {
		t.Errorf("Genres = %+v", m.Genres)
	}
	if m.Certification != "PG-13" {
		t.Errorf("Certification = %q, want US theatrical PG-13", m.Certification)
	}
	if len(m.Cast) != 2 || m.Cast[0].Character != "Cobb" || m.Cast[0].ProfileURL() != "https://image.tmdb.org/t/p/w185/leo.jpg" || m.Cast[1].ProfileURL() != "" {
		t.Errorf("Cast = %+v", m.Cast)
	}
	if len(m.Directors) != 1 || m.Directors[0].Name != "Christopher Nolan" {
		t.Errorf("Directors = %+v", m.Directors)
	}
	if len(m.Writers) != 1 || m.Writers[0].Job != "Screenplay" {
		t.Errorf("Writers = %+v", m.Writers)
	}
	if len(m.Videos) != 1 || m.Videos[0].Key != "YoHD9XEInc0" || m.TrailerKey != "YoHD9XEInc0" {
		t.Errorf("Videos = %+v, TrailerKey = %q; want only the YouTube trailer", m.Videos, m.TrailerKey)
	}
	if m.Homepage != "https://www.warnerbros.com/movies/inception" {
		t.Errorf("Homepage = %q", m.Homepage)
	}
	if want := (ExternalIDs{IMDb: "tt1375666", Wikidata: "Q25188", Facebook: "inception", Instagram: "inceptionmovie"}); m.Links != want {
		t.Errorf("Links = %+v, want %+v", m.Links, want)
	}
	if m.LogoURL() != "https://image.tmdb.org/t/p/w500/logo.png" {
		t.Errorf("LogoURL() = %q", m.LogoURL())
	}
	if len(m.Recommendations) != 1 || m.Recommendations[0].Title != "Interstellar" || m.Recommendations[0].MediaType != "movie" {
		t.Errorf("Recommendations = %+v", m.Recommendations)
	}
	if len(m.Similar) != 1 || m.Similar[0].Title != "The Prestige" || m.Similar[0].MediaType != "movie" {
		t.Errorf("Similar = %+v", m.Similar)
	}
}

func TestUnknownTitleReportsNotFound(t *testing.T) {
	client, _ := fakeTMDB(t, nil)

	_, err := client.Movie(context.Background(), 1)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Movie() error = %v, want ErrNotFound", err)
	}
}

func TestDetailsAreCachedUntilTTLExpires(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte(movieJSON))
	}))
	defer server.Close()
	client := NewClient("test-key", WithBaseURL(server.URL), WithHTTPClient(server.Client()),
		WithCacheTTL(time.Minute), WithClock(func() time.Time { return now }))

	for range 3 {
		if _, err := client.Movie(context.Background(), 27205); err != nil {
			t.Fatalf("Movie(): %v", err)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("requests within TTL = %d, want 1", hits.Load())
	}

	now = now.Add(2 * time.Minute)
	if _, err := client.Movie(context.Background(), 27205); err != nil {
		t.Fatalf("Movie(): %v", err)
	}
	if hits.Load() != 2 {
		t.Fatalf("requests after TTL = %d, want 2", hits.Load())
	}
}

func TestConcurrentDetailMissesShareOneRequest(t *testing.T) {
	release := make(chan struct{})
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		<-release
		_, _ = w.Write([]byte(movieJSON))
	}))
	defer server.Close()
	client := NewClient("test-key", WithBaseURL(server.URL), WithHTTPClient(server.Client()))

	const callers = 8
	errs := make(chan error, callers)
	for range callers {
		go func() {
			_, err := client.Movie(context.Background(), 27205)
			errs <- err
		}()
	}
	for hits.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond) // let the other callers join the in-flight request
	close(release)
	for range callers {
		if err := <-errs; err != nil {
			t.Fatalf("Movie(): %v", err)
		}
	}
	if hits.Load() != 1 {
		t.Fatalf("requests = %d, want 1", hits.Load())
	}
}

const tvJSON = `{
	"id": 1396, "name": "Breaking Bad", "original_name": "Breaking Bad",
	"tagline": "Change the equation.", "overview": "A chemistry teacher turns to crime.",
	"poster_path": "/bb.jpg", "backdrop_path": "/bb-backdrop.jpg",
	"first_air_date": "2008-01-20", "last_air_date": "2013-09-29", "status": "Ended",
	"number_of_seasons": 5, "number_of_episodes": 62, "episode_run_time": [45, 47],
	"vote_average": 8.9, "vote_count": 15000,
	"genres": [{"id": 18, "name": "Drama"}],
	"created_by": [{"id": 66633, "name": "Vince Gilligan", "profile_path": "/vince.jpg"}],
	"networks": [{"id": 174, "name": "AMC"}],
	"content_ratings": {"results": [{"iso_3166_1": "DE", "rating": "16"}, {"iso_3166_1": "US", "rating": "TV-MA"}]},
	"external_ids": {"imdb_id": "tt0903747", "tvdb_id": 81189, "instagram_id": "breakingbad", "twitter_id": "BreakingBad"},
	"homepage": "https://www.sonypictures.com/tv/breakingbad",
	"seasons": [
		{"season_number": 0, "name": "Specials", "episode_count": 9, "air_date": "2009-02-17"},
		{"season_number": 1, "name": "Season 1", "episode_count": 7, "air_date": "2008-01-20", "poster_path": "/s1.jpg"},
		{"season_number": 2, "name": "Season 2", "episode_count": 13, "air_date": "2009-03-08"}
	],
	"aggregate_credits": {"cast": [
		{"id": 17419, "name": "Bryan Cranston", "profile_path": "/bryan.jpg", "roles": [{"character": "Walter White", "episode_count": 62}]}
	]},
	"videos": {"results": [{"key": "HhesaQXLuRY", "name": "Trailer", "site": "YouTube", "type": "Trailer", "official": true}]},
	"images": {"logos": []},
	"recommendations": {"results": [{"id": 60059, "name": "Better Call Saul", "first_air_date": "2015-02-08"}]},
	"similar": {"results": []}
}`

const seasonJSON = `{
	"name": "Season 1", "overview": "High school chemistry teacher Walter White...", "season_number": 1,
	"air_date": "2008-01-20", "poster_path": "/s1.jpg",
	"episodes": [
		{"episode_number": 1, "season_number": 1, "name": "Pilot", "overview": "Walter is diagnosed.", "air_date": "2008-01-20", "runtime": 58, "still_path": "/pilot.jpg", "vote_average": 8.2},
		{"episode_number": 2, "season_number": 1, "name": "Cat's in the Bag...", "air_date": "2008-01-27", "runtime": 48}
	]
}`

func TestTVDetailsListRegularSeasonsBeforeSpecials(t *testing.T) {
	client, _ := fakeTMDB(t, map[string]string{"/3/tv/1396": tvJSON})

	tv, err := client.TV(context.Background(), 1396)
	if err != nil {
		t.Fatalf("TV(): %v", err)
	}
	if tv.Title != "Breaking Bad" || tv.MediaType != "tv" || tv.ReleaseYear() != "2008" || tv.ImdbID != "tt0903747" {
		t.Errorf("basic fields = %+v", tv.MediaItem)
	}
	if tv.Homepage != "https://www.sonypictures.com/tv/breakingbad" || tv.Links.Instagram != "breakingbad" || tv.Links.Twitter != "BreakingBad" || tv.Links.IMDb != "tt0903747" {
		t.Errorf("homepage/links = %q %+v", tv.Homepage, tv.Links)
	}
	if tv.ContentRating != "TV-MA" || tv.NumberOfSeasons != 5 || tv.NumberOfEpisodes != 62 {
		t.Errorf("rating/counts = %q %d %d", tv.ContentRating, tv.NumberOfSeasons, tv.NumberOfEpisodes)
	}
	if got := tv.YearRange(); got != "2008–2013" {
		t.Errorf("YearRange() = %q, want 2008–2013", got)
	}
	if got := tv.FormattedEpisodeRuntime(); got != "45m" {
		t.Errorf("FormattedEpisodeRuntime() = %q, want 45m", got)
	}
	if len(tv.Creators) != 1 || tv.Creators[0].Name != "Vince Gilligan" || len(tv.Networks) != 1 || tv.Networks[0] != "AMC" {
		t.Errorf("creators/networks = %+v %+v", tv.Creators, tv.Networks)
	}
	if len(tv.Seasons) != 3 || tv.Seasons[0].Number != 1 || tv.Seasons[2].Number != 0 {
		t.Fatalf("Seasons = %+v, want 1, 2, then Specials", tv.Seasons)
	}
	if tv.Seasons[0].PosterURL() != "https://image.tmdb.org/t/p/w500/s1.jpg" {
		t.Errorf("season poster = %q", tv.Seasons[0].PosterURL())
	}
	if tv.DefaultSeason() != 1 {
		t.Errorf("DefaultSeason() = %d, want 1", tv.DefaultSeason())
	}
	if len(tv.Cast) != 1 || tv.Cast[0].Character != "Walter White" {
		t.Errorf("Cast = %+v", tv.Cast)
	}
	if tv.TrailerKey != "HhesaQXLuRY" || len(tv.Recommendations) != 1 || tv.Recommendations[0].MediaType != "tv" {
		t.Errorf("trailer/recommendations = %q %+v", tv.TrailerKey, tv.Recommendations)
	}
}

func TestSeasonListsEpisodes(t *testing.T) {
	client, _ := fakeTMDB(t, map[string]string{"/3/tv/1396/season/1": seasonJSON})

	s, err := client.Season(context.Background(), 1396, 1)
	if err != nil {
		t.Fatalf("Season(): %v", err)
	}
	if s.Number != 1 || s.Name != "Season 1" || len(s.Episodes) != 2 {
		t.Fatalf("season = %+v", s)
	}
	pilot := s.Episodes[0]
	if pilot.Name != "Pilot" || pilot.Code() != "S01E01" || pilot.FormattedRuntime() != "58m" {
		t.Errorf("pilot = %+v code=%q", pilot, pilot.Code())
	}
	if pilot.StillURL() != "https://image.tmdb.org/t/p/w300/pilot.jpg" || s.Episodes[1].StillURL() != "" {
		t.Errorf("stills = %q %q", pilot.StillURL(), s.Episodes[1].StillURL())
	}
}

const personJSON = `{
	"id": 6193, "name": "Leonardo DiCaprio", "known_for_department": "Acting",
	"biography": "Born in Los Angeles.\n\nFounded a foundation.",
	"birthday": "1974-11-11", "deathday": null, "place_of_birth": "Los Angeles, California, USA",
	"profile_path": "/leo.jpg", "imdb_id": "nm0000138", "homepage": null,
	"external_ids": {"imdb_id": "nm0000138", "instagram_id": "leonardodicaprio", "tiktok_id": null, "youtube_id": "@LeonardoDiCaprio"},
	"combined_credits": {
		"cast": [
			{"id": 27205, "media_type": "movie", "title": "Inception", "character": "Cobb", "release_date": "2010-07-15", "poster_path": "/inception.jpg", "vote_count": 37000},
			{"id": 597, "media_type": "movie", "title": "Titanic", "character": "Jack Dawson", "release_date": "1997-11-18", "poster_path": "/titanic.jpg", "vote_count": 25000},
			{"id": 1111, "media_type": "movie", "title": "Untitled Project", "character": "", "release_date": "", "vote_count": 0},
			{"id": 2222, "media_type": "tv", "name": "Growing Pains", "character": "Luke Brower", "first_air_date": "1985-09-24", "poster_path": "/gp.jpg", "vote_count": 300},
			{"id": 3333, "media_type": "movie", "title": "No Poster", "character": "Himself", "release_date": "2019-01-01", "vote_count": 90000}
		],
		"crew": [
			{"id": 27205, "media_type": "movie", "title": "Inception", "job": "Producer", "release_date": "2010-07-15", "poster_path": "/inception.jpg", "vote_count": 37000}
		]
	}
}`

func TestPersonProfileHighlightsKnownForTitlesAndMergesCredits(t *testing.T) {
	client, _ := fakeTMDB(t, map[string]string{"/3/person/6193": personJSON})

	p, err := client.Person(context.Background(), 6193)
	if err != nil {
		t.Fatalf("Person(): %v", err)
	}
	if p.Name != "Leonardo DiCaprio" || p.ProfileURL() != "https://image.tmdb.org/t/p/h632/leo.jpg" || p.KnownForDepartment != "Acting" {
		t.Errorf("profile = %+v", p)
	}
	if p.Links.Instagram != "leonardodicaprio" || p.Links.YouTube != "@LeonardoDiCaprio" || p.Links.IMDb != "nm0000138" {
		t.Errorf("Links = %+v", p.Links)
	}
	if paras := p.BiographyParagraphs(); len(paras) != 2 || paras[1] != "Founded a foundation." {
		t.Errorf("BiographyParagraphs() = %q", paras)
	}
	var knownFor []string
	for _, m := range p.KnownFor {
		knownFor = append(knownFor, m.Title)
	}
	if want := []string{"Inception", "Titanic", "Growing Pains"}; !slicesEqual(knownFor, want) {
		t.Errorf("KnownFor = %q, want %q (unique, with posters, most voted first)", knownFor, want)
	}

	var movies []string
	for _, c := range p.MovieCredits() {
		movies = append(movies, c.Title+"/"+c.Role)
	}
	if want := []string{"No Poster/Himself", "Inception/Cobb, Producer", "Titanic/Jack Dawson", "Untitled Project/"}; !slicesEqual(movies, want) {
		t.Errorf("MovieCredits() = %q, want one row per title, newest first, undated last", movies)
	}
	if tv := p.TVCredits(); len(tv) != 1 || tv[0].Title != "Growing Pains" || tv[0].MediaType != "tv" {
		t.Errorf("TVCredits() = %+v", tv)
	}
}

const searchJSON = `{"results": [
	{"id": 27205, "media_type": "movie", "title": "Inception", "release_date": "2010-07-15"},
	{"id": 6193, "media_type": "person", "name": "Leonardo DiCaprio", "profile_path": "/leo.jpg", "known_for_department": "Acting",
	 "known_for": [{"media_type": "movie", "title": "Inception"}, {"media_type": "tv", "name": "Growing Pains"}]},
	{"id": 1396, "media_type": "tv", "name": "Inception: The Series", "first_air_date": "2020-01-01"}
]}`

func TestSearchGroupsMoviesSeriesAndPeople(t *testing.T) {
	var gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/3/search/multi" {
			http.NotFound(w, r)
			return
		}
		gotQuery = r.URL.Query().Get("query")
		if r.URL.Query().Get("include_adult") != "false" {
			t.Errorf("include_adult = %q, want false", r.URL.Query().Get("include_adult"))
		}
		_, _ = w.Write([]byte(searchJSON))
	}))
	defer server.Close()
	client := NewClient("test-key", WithBaseURL(server.URL), WithHTTPClient(server.Client()))

	res, err := client.Search(context.Background(), "  inception ")
	if err != nil {
		t.Fatalf("Search(): %v", err)
	}
	if gotQuery != "inception" {
		t.Errorf("query sent = %q, want trimmed", gotQuery)
	}
	if len(res.Movies) != 1 || res.Movies[0].Title != "Inception" {
		t.Errorf("Movies = %+v", res.Movies)
	}
	if len(res.Series) != 1 || res.Series[0].MediaType != "tv" {
		t.Errorf("Series = %+v", res.Series)
	}
	if len(res.People) != 1 || res.People[0].Name != "Leonardo DiCaprio" || res.People[0].ProfileURL() != "https://image.tmdb.org/t/p/w185/leo.jpg" {
		t.Fatalf("People = %+v", res.People)
	}
	if got := res.People[0].KnownForTitles(); got != "Inception, Growing Pains" {
		t.Errorf("KnownForTitles() = %q", got)
	}
}

func TestBlankSearchSkipsTheNetwork(t *testing.T) {
	client, hits := fakeTMDB(t, nil)

	res, err := client.Search(context.Background(), "   ")
	if err != nil || !res.Empty() {
		t.Fatalf("Search(blank) = %+v, %v; want empty result", res, err)
	}
	if hits.Load() != 0 {
		t.Errorf("requests = %d, want 0", hits.Load())
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCuratedListsNormalizeMediaTypeAndAreCached(t *testing.T) {
	client, hits := fakeTMDB(t, map[string]string{
		"/3/movie/now_playing": `{"results": [{"id": 1, "title": "In Theaters", "poster_path": "/a.jpg"}]}`,
		"/3/tv/top_rated":      `{"results": [{"id": 2, "name": "Acclaimed Show", "first_air_date": "2019-05-01"}]}`,
	})

	movies, err := client.List(context.Background(), NowPlayingMovies)
	if err != nil || len(movies) != 1 || movies[0].Title != "In Theaters" || movies[0].MediaType != "movie" {
		t.Fatalf("List(NowPlayingMovies) = %+v, %v", movies, err)
	}
	series, err := client.List(context.Background(), TopRatedSeries)
	if err != nil || len(series) != 1 || series[0].Title != "Acclaimed Show" || series[0].MediaType != "tv" || series[0].ReleaseYear() != "2019" {
		t.Fatalf("List(TopRatedSeries) = %+v, %v", series, err)
	}
	if _, err := client.List(context.Background(), NowPlayingMovies); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 2 {
		t.Errorf("requests = %d, want 2 (second NowPlaying served from cache)", hits.Load())
	}
}

func TestRecommendationsAreOrderedNewestFirst(t *testing.T) {
	const related = `{"results": [
		{"id": 1, "title": "Middle", "release_date": "2014-11-05"},
		{"id": 2, "title": "Unreleased", "release_date": ""},
		{"id": 3, "title": "Newest", "release_date": "2023-07-19"},
		{"id": 4, "title": "Oldest", "release_date": "2006-10-17"}
	]}`
	client, _ := fakeTMDB(t, map[string]string{
		"/3/movie/1": `{"id": 1, "title": "Base", "recommendations": ` + related + `, "similar": ` + related + `}`,
		"/3/tv/2":    `{"id": 2, "name": "Show", "recommendations": ` + related + `}`,
	})

	titles := func(items []MediaItem) string {
		var out []string
		for _, m := range items {
			out = append(out, m.Title)
		}
		return strings.Join(out, ",")
	}
	const want = "Newest,Middle,Oldest,Unreleased"
	m, err := client.Movie(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := titles(m.Recommendations); got != want {
		t.Errorf("movie Recommendations = %s, want %s", got, want)
	}
	if got := titles(m.Similar); got != want {
		t.Errorf("movie Similar = %s, want %s", got, want)
	}
	tv, err := client.TV(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := titles(tv.Recommendations); got != want {
		t.Errorf("tv Recommendations = %s, want %s", got, want)
	}
}

func TestKnownForShowsMostVotedTitlesNewestFirst(t *testing.T) {
	client, _ := fakeTMDB(t, map[string]string{"/3/person/1": `{"id": 1, "name": "Actor", "known_for_department": "Acting",
		"combined_credits": {"cast": [
			{"id": 10, "media_type": "movie", "title": "Classic", "release_date": "1996-02-23", "poster_path": "/a.jpg", "vote_count": 9000},
			{"id": 11, "media_type": "tv", "name": "Recent Show", "first_air_date": "2023-01-15", "poster_path": "/b.jpg", "vote_count": 500},
			{"id": 12, "media_type": "movie", "title": "Middle", "release_date": "2007-11-09", "poster_path": "/c.jpg", "vote_count": 7000}
		]}}`})

	p, err := client.Person(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range p.KnownFor {
		got = append(got, m.Title)
	}
	if strings.Join(got, ",") != "Recent Show,Middle,Classic" {
		t.Errorf("KnownFor = %q, want newest first", got)
	}
}

func TestFailedRequestsNeverExposeTheAPIKey(t *testing.T) {
	const v3Key = "fedcba9876543210fedcba9876543210" // #nosec G101 -- a made-up v3 key
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close() // every request now fails at the transport, as on a network outage

	client := NewClient(v3Key, WithBaseURL(server.URL))
	_, movieErr := client.Movie(context.Background(), 27205)
	_, catalogErr := client.GetCatalog(context.Background())

	for name, err := range map[string]error{"Movie": movieErr, "GetCatalog": catalogErr} {
		if err == nil {
			t.Fatalf("%s() error = nil, want a transport error", name)
		}
		if strings.Contains(err.Error(), v3Key) {
			t.Errorf("%s() error leaks the API key: %v", name, err)
		}
	}
}
