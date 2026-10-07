package handlers

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/auth"
	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/jacred"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"

	"github.com/a-h/templ"
	"github.com/starfederation/datastar-go/datastar"
)

const (
	torrentSearchTimeout = 20 * time.Second
	torrStatsTimeout     = 3 * time.Second
)

var errDetailsUnavailable = errors.New("tmdb details provider not configured")

// pageUser returns the signed-in user or redirects to the sign-in flow.
func (c *Catalog) pageUser(w http.ResponseWriter, r *http.Request) *auth.User {
	user := c.userFromRequest(r)
	if user == nil {
		http.Redirect(w, r, c.signIn, http.StatusSeeOther)
	}
	return user
}

// apiUser returns the signed-in user or answers 401.
func (c *Catalog) apiUser(w http.ResponseWriter, r *http.Request) *auth.User {
	user := c.userFromRequest(r)
	if user == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	}
	return user
}

// positiveInt parses a TMDB id or season number.
func positiveInt(raw string) (int, bool) {
	n, err := strconv.Atoi(raw)
	return n, err == nil && n > 0
}

func (c *Catalog) detailsContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), c.catalogTimeout)
}

// renderMediaError answers a failed TMDB lookup with a 404 or 502 page.
func renderMediaError(w http.ResponseWriter, r *http.Request, user *auth.User, err error) {
	ctx := r.Context()
	status, heading, message := http.StatusBadGateway, i18n.T(ctx, "TMDB is unavailable"), i18n.T(ctx, "Movie and TV metadata could not be loaded from TMDB right now. Please try again in a moment.")
	if errors.Is(err, tmdb.ErrNotFound) {
		status, heading, message = http.StatusNotFound, i18n.T(ctx, "Title not found"), i18n.T(ctx, "TMDB has no entry for this address.")
	} else {
		slog.Warn("tmdb details lookup failed", "path", logPath(r.URL.Path), "error", logError(err))
	}
	templ.Handler(views.MediaError(user, heading, message), templ.WithStatus(status)).ServeHTTP(w, r)
}

// detailTarget resolves the signed-in user and the {id} of a title page. It
// answers the request itself (redirect, 404 or 502) and returns ok=false when
// the page cannot be served.
func (c *Catalog) detailTarget(w http.ResponseWriter, r *http.Request) (*auth.User, int, bool) {
	user := c.pageUser(w, r)
	if user == nil {
		return nil, 0, false
	}
	id, ok := positiveInt(r.PathValue("id"))
	if !ok {
		renderMediaError(w, r, user, tmdb.ErrNotFound)
		return nil, 0, false
	}
	if c.clients().Details == nil {
		renderMediaError(w, r, user, errDetailsUnavailable)
		return nil, 0, false
	}
	return user, id, true
}

func (c *Catalog) handleMoviePage(w http.ResponseWriter, r *http.Request) {
	user, id, ok := c.detailTarget(w, r)
	if !ok {
		return
	}
	ctx, cancel := c.detailsContext(r)
	defer cancel()
	movie, err := c.clients().Details.Movie(ctx, id)
	if err != nil {
		renderMediaError(w, r, user, err)
		return
	}
	templ.Handler(views.MoviePage(user, movie)).ServeHTTP(w, r)
}

func (c *Catalog) handlePersonPage(w http.ResponseWriter, r *http.Request) {
	user, id, ok := c.detailTarget(w, r)
	if !ok {
		return
	}
	ctx, cancel := c.detailsContext(r)
	defer cancel()
	person, err := c.clients().Details.Person(ctx, id)
	if err != nil {
		renderMediaError(w, r, user, err)
		return
	}
	templ.Handler(views.PersonPage(user, person)).ServeHTTP(w, r)
}

// titleInfo is what torrent search and TorrServer need to know about a title.
type titleInfo struct {
	id     int
	query  jacred.Query
	label  string
	poster string
	tv     *tmdb.TVDetails // set for series
	// minutes a movie runs, and how many episodes the searched season has:
	// what a release's bitrate is estimated over.
	minutes, seasonEpisodes int
}

// withRuntimes is a copy of results (which JacRed's cache shares), each
// timed by how long it plays: the movie, or the season's episodes it holds.
func (t titleInfo) withRuntimes(results []jacred.Result) []jacred.Result {
	out := slices.Clone(results)
	for i, r := range out {
		minutes := t.minutes
		if t.tv != nil {
			minutes = t.tv.EpisodeRuntime * r.Episodes(t.seasonEpisodes)
		}
		out[i].Runtime = time.Duration(minutes) * time.Minute
	}
	return out
}

// lookupTitle resolves ?type=movie|tv&id=N through TMDB, so clients can only
// search for real titles.
func (c *Catalog) lookupTitle(ctx context.Context, r *http.Request) (titleInfo, string, error) {
	mediaType := r.URL.Query().Get("type")
	id, ok := positiveInt(r.URL.Query().Get("id"))
	if !ok || (mediaType != "movie" && mediaType != "tv") {
		return titleInfo{}, mediaType, tmdb.ErrNotFound
	}
	if c.clients().Details == nil {
		return titleInfo{}, mediaType, errDetailsUnavailable
	}
	if mediaType == "tv" {
		tv, err := c.clients().Details.TV(ctx, id)
		if err != nil {
			return titleInfo{}, mediaType, err
		}
		year, _ := strconv.Atoi(tv.ReleaseYear())
		return titleInfo{
			id:     id,
			query:  jacred.Query{Title: tv.Title, OriginalTitle: tv.OriginalTitle, Year: year},
			label:  views.PageTitle(tv.MediaItem),
			poster: tv.PosterURL(),
			tv:     tv,
		}, mediaType, nil
	}
	movie, err := c.clients().Details.Movie(ctx, id)
	if err != nil {
		return titleInfo{}, mediaType, err
	}
	year, _ := strconv.Atoi(movie.ReleaseYear())
	return titleInfo{
		id:      id,
		query:   jacred.Query{Title: movie.Title, OriginalTitle: movie.OriginalTitle, Year: year},
		label:   views.PageTitle(movie.MediaItem),
		poster:  movie.PosterURL(),
		minutes: movie.Runtime,
	}, mediaType, nil
}

// sortParam returns a supported result order, defaulting to seeders.
func sortParam(r *http.Request) string {
	switch sort := r.URL.Query().Get("sort"); sort {
	case "date", "size", "bitrate":
		return sort
	default:
		return "seeders"
	}
}

// qualitiesParam reads ?quality=2160,1080: the qualities JacRed files
// releases under, anything else dropped.
func qualitiesParam(r *http.Request) []int {
	var out []int
	for v := range strings.SplitSeq(r.URL.Query().Get("quality"), ",") {
		switch n, _ := strconv.Atoi(v); n {
		case 2160, 1080, 720, 480:
			if !slices.Contains(out, n) {
				out = append(out, n)
			}
		}
	}
	return out
}

// scopeToSeason narrows a series search to ?season=N, dated by the show's
// first year and the season's own air year.
func scopeToSeason(title *titleInfo, r *http.Request) bool {
	n, err := strconv.Atoi(r.URL.Query().Get("season"))
	if err != nil {
		return false
	}
	for _, season := range title.tv.Seasons {
		if season.Number == n {
			title.query.Season = n
			title.seasonEpisodes = season.EpisodeCount
			if len(season.AirDate) >= 4 {
				title.query.SeasonYear, _ = strconv.Atoi(season.AirDate[:4])
			}
			title.label += " · Season " + strconv.Itoa(n)
			return true
		}
	}
	return false
}

// handleTorrentSearch runs only when the user asks for sources. It streams a
// searching state immediately, then the sorted JacRed results (or an
// explanation) once the upstream answers.
func (c *Catalog) handleTorrentSearch(w http.ResponseWriter, r *http.Request) {
	if c.apiUser(w, r) == nil {
		return
	}
	sse := datastar.NewSSE(w, r)
	ctx, cancel := context.WithTimeout(r.Context(), torrentSearchTimeout)
	defer cancel()

	title, mediaType, err := c.lookupTitle(ctx, r)
	if err != nil {
		patchTorrentError(r, sse, "This title could not be loaded from TMDB, so sources cannot be searched.")
		return
	}
	if title.tv != nil && !scopeToSeason(&title, r) {
		patchTorrentError(r, sse, "Choose a season to search for sources.")
		return
	}
	if err := sse.PatchElementTempl(views.TorrentSearching(title.label)); err != nil {
		logSSEError(r, "patch torrent searching", err)
		return
	}
	if c.clients().Torrents == nil {
		patchTorrentError(r, sse, "Torrent search is not configured on this server.")
		return
	}
	title.query.Qualities, title.query.HDR = qualitiesParam(r), r.URL.Query().Get("hdr") == "1"
	results, err := c.clients().Torrents.Search(ctx, title.query)
	if err != nil {
		slog.Warn("jacred search failed", "title", title.label, "error", err)
		patchTorrentError(r, sse, jacredProblem(r.Context(), err))
		return
	}
	sort := sortParam(r)
	search := views.SourcesSearch(mediaType, title.id)
	if err := sse.PatchElementTempl(views.TorrentResults(jacred.Sort(title.withRuntimes(results), sort), sort, search)); err != nil {
		logSSEError(r, "patch torrent results", err)
	}
}

// jacredProblem says why a JacRed search failed, and what to do about it.
func jacredProblem(ctx context.Context, err error) string {
	if views.IsCloud(ctx) && (errors.Is(err, jacred.ErrKeyNeeded) || errors.Is(err, jacred.ErrBlocked)) {
		return i18n.T(ctx, "JacRed is unavailable right now. Please try again later.")
	}
	var limit *jacred.LimitError
	switch {
	case errors.Is(err, jacred.ErrKeyNeeded):
		return i18n.T(ctx, "JacRed needs a key to search. An administrator adds a free one in Settings → Sources.")
	case errors.Is(err, jacred.ErrBlocked):
		return i18n.T(ctx, "JacRed has blocked the account of this server's key. An administrator can check it at jacred.su.")
	case errors.As(err, &limit):
		if limit.Retry <= 0 {
			return i18n.T(ctx, "Today's JacRed searches are used up. Please try again later.")
		}
		return i18n.Tf(ctx, "Today's JacRed searches are used up. Please try again in %s.", waitText(ctx, limit.Retry))
	default:
		return i18n.T(ctx, "JacRed is not responding right now. Please try again later.")
	}
}

// waitText says a wait the way people do: "20 minutes", "about 3 hours".
func waitText(ctx context.Context, d time.Duration) string {
	hours, minutes := int(d.Round(time.Hour)/time.Hour), max(1, int(d.Round(time.Minute)/time.Minute))
	switch {
	case d >= 90*time.Minute:
		return i18n.N(ctx, hours, "about %d hour", "about %d hours")
	case d >= 50*time.Minute:
		return i18n.T(ctx, "about an hour")
	default:
		return i18n.N(ctx, minutes, "%d minute", "%d minutes")
	}
}

func patchTorrentError(r *http.Request, sse *datastar.ServerSentEventGenerator, message string) {
	if err := sse.PatchElementTempl(views.TorrentSearchError(message)); err != nil {
		logSSEError(r, "patch torrent error", err)
	}
}

type torrentAddSignals struct {
	Magnet string `json:"torrMagnet"`
}

// handleTorrentAdd sends a release to the active TorrServer, titled and
// postered from TMDB, then starts a live progress card for it.
func (s *Server) handleTorrentAdd(w http.ResponseWriter, r *http.Request) {
	if s.apiUser(w, r) == nil {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	var signals torrentAddSignals
	if err := datastar.ReadSignals(r, &signals); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	sse := datastar.NewSSE(w, r)
	hash := magnetInfoHash(signals.Magnet)
	if hash == "" {
		patchToast(r, sse, "That is not a magnet link.", true)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	title, mediaType, err := s.lookupTitle(ctx, r)
	if err != nil {
		slog.Warn("torrent add title lookup failed", "error", err)
	}
	if err := s.torrServer.Client().AddTorrent(ctx, signals.Magnet, title.label, title.poster, mediaType); err != nil {
		slog.Warn("torrserver add failed", "error", err)
		patchToast(r, sse, "TorrServer did not accept the release. Check that it is running.", true)
		return
	}
	if title.id > 0 && (mediaType == "movie" || mediaType == "tv") {
		s.rememberTitle(hash, mediaType, title.id)
	}
	if err := sse.PatchElementTempl(views.TorrActivity(hash, cmp.Or(title.label, "New torrent"))); err != nil {
		logSSEError(r, "patch torrent activity", err)
		return
	}
	patchToast(r, sse, "Added to TorrServer", false)
}

func patchToast(r *http.Request, sse *datastar.ServerSentEventGenerator, message string, isError bool) {
	if err := sse.MarshalAndPatchSignals(map[string]any{"toast": message, "toastError": isError}); err != nil {
		logSSEError(r, "patch toast", err)
	}
}

// magnetInfoHash returns the lowercase hex BTIH of a magnet URI, or "".
func magnetInfoHash(magnet string) string {
	if !strings.HasPrefix(magnet, "magnet:?") {
		return ""
	}
	_, rest, ok := strings.Cut(magnet, "xt=urn:btih:")
	if !ok {
		return ""
	}
	hash, _, _ := strings.Cut(rest, "&")
	if !isHexHash(hash) {
		return ""
	}
	return strings.ToLower(hash)
}

func isHexHash(hash string) bool {
	if len(hash) != 40 {
		return false
	}
	for _, c := range hash {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

// handleTorrServerStatus reports whether the active TorrServer answers.
func (s *Server) handleTorrServerStatus(w http.ResponseWriter, r *http.Request) {
	if s.apiUser(w, r) == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	endpoint := s.torrServer.ActiveURL()
	echo, err := s.torrServer.Client().Echo(ctx)
	online := err == nil && echo.Version != ""
	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(views.TorrServerStatus(online, echo.Version, cmp.Or(endpoint, "not configured"))); err != nil {
		logSSEError(r, "patch torrserver status", err)
	}
}

// handleTorrServerState sets the navbar's $_tsNav: whether the active
// TorrServer answers.
func (s *Server) handleTorrServerState(w http.ResponseWriter, r *http.Request) {
	if s.apiUser(w, r) == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	state := "offline"
	if echo, err := s.torrServer.Client().Echo(ctx); err == nil && echo.Version != "" {
		state = "online"
	}
	if err := datastar.NewSSE(w, r).MarshalAndPatchSignals(map[string]string{"_tsNav": state}); err != nil {
		logSSEError(r, "patch torrserver state", err)
	}
}

// handleTorrentStats shows one torrent's download stats. With stream=true it
// follows the shared torrent list until the page closes, making no TorrServer
// requests of its own.
func (s *Server) handleTorrentStats(w http.ResponseWriter, r *http.Request) {
	if s.apiUser(w, r) == nil {
		return
	}
	hash := strings.ToLower(r.URL.Query().Get("hash"))
	if !isHexHash(hash) {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("stream") != "true" {
		ctx, cancel := context.WithTimeout(r.Context(), torrStatsTimeout)
		defer cancel()
		t, err := s.torrServer.Client().TorrentStats(ctx, hash)
		if err != nil {
			t = nil
		}
		if err := datastar.NewSSE(w, r).PatchElementTempl(views.TorrActivityStats(t)); err != nil {
			logSSEError(r, "patch torrent stats", err)
		}
		return
	}
	if !s.acquireSSE() {
		w.Header().Set("Retry-After", "2")
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return
	}
	defer s.releaseSSE()

	sse := datastar.NewSSE(w, r)
	ctx, cancel := s.streamContext(r)
	defer cancel()
	sub := s.live.Subscribe(topicTorrents)
	defer sub.Close()
	var last string
	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.C:
		}
		var found *torrserver.Torrent
		for _, t := range liveValue[torrentsState](sub, topicTorrents).List {
			if strings.EqualFold(t.Hash, hash) {
				found = &t
				break
			}
		}
		if err := patchIfChanged(ctx, sse, views.TorrActivityStats(found), &last); err != nil {
			logSSEError(r, "patch torrent stats stream", err)
			return
		}
	}
}
