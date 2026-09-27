package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/auth"
	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/engine"
	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/jacred"
	"github.com/lieranderl/moviestracker-app/internal/tmdb"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"

	"github.com/a-h/templ"
	"github.com/starfederation/datastar-go/datastar"
)

// sourceCheckTimeout bounds the live check a source gets before it is saved.
const sourceCheckTimeout = 15 * time.Second

// adminPage returns the signed-in admin, or answers the request itself:
// signed-out visitors go to sign-in, other accounts get 403.
func (s *Server) adminPage(w http.ResponseWriter, r *http.Request) *auth.User {
	user := s.pageUser(w, r)
	if user != nil && !user.IsAdmin() {
		http.Error(w, "Only an administrator can change settings.", http.StatusForbidden)
		return nil
	}
	return user
}

// adminAPI is adminPage for actions: 401 when signed out.
func (s *Server) adminAPI(w http.ResponseWriter, r *http.Request) *auth.User {
	user := s.apiUser(w, r)
	if user != nil && !user.IsAdmin() {
		http.Error(w, "Only an administrator can change settings.", http.StatusForbidden)
		return nil
	}
	return user
}

// sourcesView describes the saved sources without revealing any key.
func (s *Server) sourcesView() views.SourcesView {
	st := s.store.State()
	eff := s.env.Apply(st)
	envName := func(set bool, name string) string {
		if set {
			return name
		}
		return ""
	}
	jacredEnv := envName(s.env.JacRedURL != "", "JACRED_URL")
	if jacredEnv == "" {
		jacredEnv = envName(s.env.JacRedAPIKey != "", "JACRED_APIKEY")
	}
	return views.SourcesView{
		TMDBKeySet:        eff.Sources.TMDBKey != "",
		TMDBShared:        s.env.UsesSharedTMDBKey(st),
		TMDBCanShare:      s.env.SharedTMDBKey != "",
		TMDBEnv:           envName(s.env.TMDBKey != "", "TMDB_API_KEY"),
		JacRedURL:         eff.Sources.JacRedURL,
		JacRedKeySet:      st.Sources.JacRedAPIKey != "" || s.env.JacRedAPIKey != "",
		JacRedShared:      s.env.UsesSharedJacRedKey(st),
		JacRedCanShare:    s.env.SharedJacRedKey != "",
		JacRedEnv:         jacredEnv,
		IMDbOn:            !eff.Sources.IMDbOff,
		IMDbEnv:           envName(s.env.IMDbURL != "", "IMDB_SERVICE_URL"),
		TorrServerURL:     eff.TorrServer.URL,
		TorrServerUser:    eff.TorrServer.User,
		TorrServerPassSet: eff.TorrServer.Password != "",
		TorrServerEnv:     envName(s.env.TorrServerURL != "", "TORRSERVER_URL"),
		Engine:            s.engineView(),
	}
}

// engineView describes the managed engine without its address or password.
func (s *Server) engineView() views.EngineView {
	v := views.EngineView{Available: s.engine != nil, Mode: s.engineMode()}
	if s.engine == nil {
		return v
	}
	st := s.engine.Status()
	v.State, v.Version, v.Port, v.Restarts, v.LastError = string(st.State), st.Version, st.Port, st.Restarts, st.LastError
	return v
}

// engineMode is where TorrServer runs: managed when chosen (or, before any
// choice, when a TorrServer program is available), external otherwise.
func (s *Server) engineMode() string {
	if s.env.TorrServerURL != "" {
		return config.EngineExternal
	}
	switch mode := s.store.State().TorrServer.Mode; {
	case mode != "":
		return mode
	case s.engine != nil:
		return config.EngineManaged
	default:
		return config.EngineExternal
	}
}

func (s *Server) handleSourcesPage(w http.ResponseWriter, r *http.Request) {
	user := s.adminPage(w, r)
	if user == nil {
		return
	}
	v := s.sourcesView()
	v.Welcome = r.URL.Query().Get("welcome") == "1"
	v.GStreamer = s.gstNow(r.Context(), user)
	templ.Handler(views.SourcesPage(user, v)).ServeHTTP(w, r)
}

// reconnect rebuilds the TMDB/JacRed/IMDb clients from the saved sources, so
// a change takes effect on the next request.
func (s *Server) reconnect() {
	clients := s.connector.Connect(s.env.Apply(s.store.State()).Sources)
	s.current.Store(&clients)
}

// saveSources applies fn to the stored sources and reconnects.
func (s *Server) saveSources(fn func(*config.State)) error {
	err := s.store.Update(func(st *config.State) error {
		fn(st)
		return nil
	})
	if err != nil {
		return err
	}
	s.reconnect()
	return nil
}

// sourceAction reads the signals of a Sources form after checking that the
// caller is an admin and the setting is not fixed by the environment. It
// answers the request itself and returns false when the save must not go on.
func (s *Server) sourceAction(w http.ResponseWriter, r *http.Request, envVar string, signals any) bool {
	if s.adminAPI(w, r) == nil {
		return false
	}
	if envVar != "" {
		http.Error(w, "This setting is set by the "+envVar+" environment variable.", http.StatusConflict)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	if err := datastar.ReadSignals(r, signals); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return false
	}
	return true
}

func patchSource(w http.ResponseWriter, r *http.Request, section templ.Component, signals map[string]any) {
	sse := datastar.NewSSE(w, r)
	if err := sse.PatchElementTempl(section); err != nil {
		logSSEError(r, "patch source section", err)
		return
	}
	if signals != nil {
		if err := sse.MarshalAndPatchSignals(signals); err != nil {
			logSSEError(r, "patch source signals", err)
		}
	}
}

// failed is a failure message, English: the page translates format and
// formats args with it.
func failed(format string, args ...any) views.SourceStatus {
	return views.SourceStatus{Message: format, Args: args}
}

// succeeded is a success message, like failed's.
func succeeded(format string, args ...any) views.SourceStatus {
	return views.SourceStatus{OK: true, Message: format, Args: args}
}

const saveFailed = "The setting could not be saved. Check that the data directory is writable."

func (s *Server) handleSaveTMDB(w http.ResponseWriter, r *http.Request) {
	var sig struct {
		Key string `json:"tmdbKey"`
	}
	if !s.sourceAction(w, r, s.sourcesView().TMDBEnv, &sig) {
		return
	}
	key := strings.TrimSpace(sig.Key)
	status := func(st views.SourceStatus) {
		patchSource(w, r, views.TMDBSource(s.sourcesView(), st), map[string]any{"tmdbKey": ""})
	}
	if key == "" {
		status(failed("Paste your TMDB key first."))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), sourceCheckTimeout)
	defer cancel()
	if err := s.connector.CheckTMDB(ctx, key); err != nil {
		if errors.Is(err, tmdb.ErrInvalidKey) {
			status(failed("TMDB rejected this key. Copy it again from your TMDB API settings."))
			return
		}
		slog.Warn("tmdb key check failed", "error", err)
		status(failed("TMDB could not be reached, so the key was not checked or saved. Check this machine's internet connection."))
		return
	}
	if err := s.saveSources(func(st *config.State) { st.Sources.TMDBKey = key }); err != nil {
		slog.Error("save tmdb key failed", "error", err)
		status(failed(saveFailed))
		return
	}
	status(succeeded("Key saved. TMDB is ready."))
}

// handleUseSharedTMDB forgets the saved TMDB key, so the shared key is used.
func (s *Server) handleUseSharedTMDB(w http.ResponseWriter, r *http.Request) {
	var sig struct{}
	if !s.sourceAction(w, r, s.sourcesView().TMDBEnv, &sig) {
		return
	}
	status := func(st views.SourceStatus) {
		patchSource(w, r, views.TMDBSource(s.sourcesView(), st), map[string]any{"tmdbKey": ""})
	}
	if s.env.SharedTMDBKey == "" {
		status(failed("This Moviestracker has no shared TMDB key."))
		return
	}
	if err := s.saveSources(func(st *config.State) { st.Sources.TMDBKey = "" }); err != nil {
		slog.Error("forget tmdb key failed", "error", err)
		status(failed(saveFailed))
		return
	}
	status(succeeded("Moviestracker's shared key is in use again."))
}

func (s *Server) handleSaveJacRed(w http.ResponseWriter, r *http.Request) {
	var sig struct {
		URL    string `json:"jacredUrl"`
		APIKey string `json:"jacredApiKey"`
	}
	if !s.sourceAction(w, r, s.sourcesView().JacRedEnv, &sig) {
		return
	}
	status := func(st views.SourceStatus) {
		patchSource(w, r, views.JacRedSource(s.sourcesView(), st), map[string]any{"jacredApiKey": ""})
	}
	baseURL := strings.TrimRight(strings.TrimSpace(sig.URL), "/")
	apiKey := strings.TrimSpace(sig.APIKey)
	// An empty key field keeps the saved key of the same instance.
	if saved := s.store.State().Sources; apiKey == "" && baseURL == saved.JacRedURL {
		apiKey = saved.JacRedAPIKey
	}
	// Without a key of one's own, jacred.su is checked with Moviestracker's
	// project key, which is never saved as one's own.
	checkKey := apiKey
	if checkKey == "" && config.IsJacredSu(baseURL) {
		checkKey = s.env.SharedJacRedKey
	}
	ctx, cancel := context.WithTimeout(r.Context(), sourceCheckTimeout)
	defer cancel()
	found, left, err := s.connector.CheckJacRed(ctx, baseURL, checkKey)
	if err != nil {
		slog.Warn("jacred test search failed", "url", baseURL, "error", err)
		if errors.Is(err, jacred.ErrKeyNeeded) {
			status(failed("%s needs a valid key, so nothing was saved. For jacred.su, create one under «Мой ключ» at jacred.su/account and paste it here.", baseURL))
			return
		}
		status(failed("The test search at %s failed, so it was not saved: %v", baseURL, err))
		return
	}
	quota := ""
	if left >= 0 {
		quota = " " + i18n.N(r.Context(), left, "The key has %d search left today.", "The key has %d searches left today.")
	}
	if err := s.saveSources(func(st *config.State) {
		st.Sources.JacRedURL, st.Sources.JacRedAPIKey = baseURL, apiKey
	}); err != nil {
		slog.Error("save jacred failed", "error", err)
		status(failed(saveFailed))
		return
	}
	if found == 0 {
		status(succeeded("Saved. JacRed answered, but the test search found no releases; it may still be indexing.%s", quota))
		return
	}
	status(succeeded("%s%s", i18n.N(r.Context(), found, "Saved. The test search found %d release.", "Saved. The test search found %d releases."), quota))
}

// handleUseSharedJacRed forgets the saved JacRed key, so jacred.su searches
// run on Moviestracker's project key.
func (s *Server) handleUseSharedJacRed(w http.ResponseWriter, r *http.Request) {
	var sig struct{}
	if !s.sourceAction(w, r, s.sourcesView().JacRedEnv, &sig) {
		return
	}
	status := func(st views.SourceStatus) {
		patchSource(w, r, views.JacRedSource(s.sourcesView(), st), map[string]any{"jacredApiKey": ""})
	}
	if s.env.SharedJacRedKey == "" {
		status(failed("This Moviestracker has no JacRed key of its own."))
		return
	}
	if err := s.saveSources(func(st *config.State) {
		st.Sources.JacRedURL, st.Sources.JacRedAPIKey = config.DefaultJacRedURL, ""
	}); err != nil {
		slog.Error("forget jacred key failed", "error", err)
		status(failed(saveFailed))
		return
	}
	status(succeeded("Moviestracker's JacRed key is in use again: unlimited searches on jacred.su."))
}

// handleSaveIMDb switches IMDb ratings on or off. The rating service is
// Moviestracker's own (or IMDB_SERVICE_URL): Sources never shows its address.
func (s *Server) handleSaveIMDb(w http.ResponseWriter, r *http.Request) {
	var sig struct {
		On bool `json:"imdbOn"`
	}
	if !s.sourceAction(w, r, s.sourcesView().IMDbEnv, &sig) {
		return
	}
	if err := s.saveSources(func(st *config.State) { st.Sources.IMDbOff = !sig.On }); err != nil {
		slog.Error("save imdb failed", "error", err)
		patchSource(w, r, views.IMDbSource(s.sourcesView(), failed(saveFailed)), nil)
		return
	}
	st := succeeded("Saved. IMDb ratings are off.")
	if sig.On {
		st = succeeded("Saved. Title pages show IMDb ratings when the service answers.")
	}
	patchSource(w, r, views.IMDbSource(s.sourcesView(), st), nil)
}

func (s *Server) handleSaveTorrServer(w http.ResponseWriter, r *http.Request) {
	var sig struct {
		Mode     string `json:"torrserverMode"`
		URL      string `json:"torrserverUrl"`
		User     string `json:"torrserverUser"`
		Password string `json:"torrserverPassword"`
	}
	if !s.sourceAction(w, r, s.sourcesView().TorrServerEnv, &sig) {
		return
	}
	status := func(st views.SourceStatus) {
		patchSource(w, r, views.TorrServerSource(s.sourcesView(), st), map[string]any{"torrserverPassword": ""})
	}
	if sig.Mode == config.EngineManaged {
		status(s.useManagedEngine(r.Context()))
		return
	}
	address, user, password, err := torrserver.ParseServerURL(sig.URL)
	if err != nil {
		status(failed("Enter the TorrServer address as http://host:port, e.g. http://127.0.0.1:8090."))
		return
	}
	// A login typed in the fields wins over one typed in the address; an
	// empty password keeps the saved one for the same TorrServer and user.
	if u := strings.TrimSpace(sig.User); u != "" || sig.Password != "" {
		user, password = u, sig.Password
	}
	if saved := s.store.State().TorrServer; password == "" && user != "" && user == saved.User && address == saved.URL {
		password = saved.Password
	}
	ctx, cancel := context.WithTimeout(r.Context(), sourceCheckTimeout)
	defer cancel()
	client := torrserver.NewClient(address, nil, torrserver.WithBasicAuth(user, password))
	echo, err := client.Echo(ctx)
	if err != nil || echo.Version == "" {
		status(failed("No TorrServer answered at %s. Check that it is running and the address is right.", address))
		return
	}
	// /echo answers without a login; the torrent list needs one when
	// TorrServer runs with --httpauth.
	if _, err := client.ListTorrents(ctx); errors.Is(err, torrserver.ErrUnauthorized) {
		if user == "" {
			status(failed("TorrServer %s at %s asks for a username and password: enter the ones set in its accs.db.", echo.Version, address))
		} else {
			status(failed("TorrServer %s at %s refused the username or password.", echo.Version, address))
		}
		return
	}
	if err := s.saveSources(func(st *config.State) {
		st.TorrServer.Mode, st.TorrServer.URL = config.EngineExternal, address
		st.TorrServer.User, st.TorrServer.Password = user, password
	}); err != nil {
		slog.Error("save torrserver failed", "error", err)
		status(failed(saveFailed))
		return
	}
	if err := s.torrServer.SetEndpoint(address, user, password); err != nil {
		status(failed("%v", err))
		return
	}
	if s.engine != nil {
		_ = s.engine.Stop()
	}
	status(succeeded("Connected to TorrServer %s.", echo.Version))
}

// useManagedEngine starts the managed engine (if needed), points the app at
// it and saves the choice.
func (s *Server) useManagedEngine(ctx context.Context) views.SourceStatus {
	if s.engine == nil {
		return failed("The TorrServer program was not found. Run `make torrserver`, put a torrserver program next to moviestracker, or set MT_TORRSERVER_BIN.")
	}
	if state := s.engine.Status().State; state != engine.Running && state != engine.Restarting {
		ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
		if err := s.engine.Start(ctx); err != nil {
			slog.Warn("managed engine did not start", "error", err)
			return failed("TorrServer did not start: %v", err)
		}
	}
	url, user, password := s.engine.Endpoint()
	if err := s.torrServer.SetEndpoint(url, user, password); err != nil {
		return failed("%v", err)
	}
	if err := s.saveSources(func(st *config.State) { st.TorrServer.Mode = config.EngineManaged }); err != nil {
		slog.Error("save engine mode failed", "error", err)
		return failed(saveFailed)
	}
	return succeeded("Moviestracker is running TorrServer %s.", s.engine.Status().Version)
}

// handleRestartEngine restarts the managed engine.
func (s *Server) handleRestartEngine(w http.ResponseWriter, r *http.Request) {
	if s.adminAPI(w, r) == nil {
		return
	}
	if s.engine == nil || s.engineMode() != config.EngineManaged {
		http.Error(w, "No managed TorrServer to restart.", http.StatusNotFound)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	st := succeeded("TorrServer restarted.")
	if err := s.engine.Restart(ctx); err != nil {
		slog.Warn("engine restart failed", "error", err)
		st = failed("TorrServer did not restart: %v", err)
	} else {
		url, user, password := s.engine.Endpoint()
		_ = s.torrServer.SetEndpoint(url, user, password)
	}
	patchSource(w, r, views.TorrServerSource(s.sourcesView(), st), nil)
}
