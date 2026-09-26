package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"

	"github.com/a-h/templ"
	"github.com/starfederation/datastar-go/datastar"
)

const (
	settingsTimeout = 10 * time.Second
	reconnectCost   = "Saving makes TorrServer reconnect (about 2 seconds)."
	startupOnly     = "Only when Moviestracker runs TorrServer (Settings → Sources)."
	engineAsleep    = "TorrServer is not answering, so its settings cannot be shown. Check Settings → Sources."
)

func (s *Server) handleSettingsIndex(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/settings/sources", http.StatusSeeOther)
}

// handleSettingsPage serves /settings/{section}: an engine section,
// GStreamer or Security.
func (s *Server) handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	user := s.adminPage(w, r)
	if user == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), settingsTimeout)
	defer cancel()
	switch id := r.PathValue("section"); id {
	case "gstreamer":
		templ.Handler(views.SettingsPage(user, s.gstreamerView(ctx))).ServeHTTP(w, r)
	case "security":
		templ.Handler(views.SecurityPage(user, views.SourceStatus{})).ServeHTTP(w, r)
	case "apps":
		if s.appsPort == nil {
			http.NotFound(w, r)
			return
		}
		templ.Handler(views.AppsPage(user, s.appsView(r), views.SourceStatus{})).ServeHTTP(w, r)
	case "users":
		templ.Handler(views.UsersPage(user, s.usersView(user))).ServeHTTP(w, r)
	default:
		sec, ok := sectionByID(id)
		if !ok {
			http.NotFound(w, r)
			return
		}
		templ.Handler(views.SettingsPage(user, s.engineSectionView(ctx, sec))).ServeHTTP(w, r)
	}
}

// managed reports whether Moviestracker runs the engine (startup options apply).
func (s *Server) managed() bool {
	return s.engine != nil && s.engineMode() == config.EngineManaged
}

// engineSectionView shows a section with TorrServer's current values.
func (s *Server) engineSectionView(ctx context.Context, sec settingsSection) views.SettingsSection {
	v := sectionView(sec, "/api/settings/engine/"+sec.ID)
	v.Cost, v.Playing = reconnectCost, int(s.playing.Load())
	sets, err := s.torrServer.Client().Settings(ctx)
	if err != nil {
		v.Unavailable = engineAsleep
		return v
	}
	startup := s.store.State().TorrServer.Startup
	for i, f := range sec.Fields {
		if f.Startup {
			v.Values[f.Key] = startupValue(startup, f)
			if !s.managed() {
				v.Fields[i].ReadOnly = startupOnly
			}
			continue
		}
		v.Values[f.Key] = shownValue(sets, f)
	}
	return v
}

// gstreamerView shows TorrServer's GStreamer settings.
func (s *Server) gstreamerView(ctx context.Context) views.SettingsSection {
	v := sectionView(gstreamerSection, "/api/settings/gstreamer")
	v.Cost, v.ResetURL = "Applied at once to new streams.", "/api/settings/gstreamer/reset"
	client := s.torrServer.Client()
	gst, err := client.GSTSettings(ctx)
	switch {
	case err != nil:
		v.Unavailable = engineAsleep
		return v
	case !gst.BuiltIn:
		v.Unavailable = "This TorrServer was built without GStreamer, so MKV files cannot be converted for the browser. " +
			"The TorrServer Moviestracker runs (make torrserver) is the GStreamer build."
		return v
	}
	echo, _ := client.Echo(ctx)
	v.Header = views.GStreamerRuntime(echo)
	for i, f := range gstreamerSection.Fields {
		v.Values[f.Key] = shownValue(gst.Config, f)
		// Without a tone mapper the setting does nothing; one left on can still be turned off.
		if f.Key == "HDRToSDR" && !echo.HDRTonemap && v.Values[f.Key] != true {
			v.Fields[i].ReadOnly = noToneMapper
		}
	}
	return v
}

const noToneMapper = "This GStreamer has no HDR tone mapper (TorrServer looks for an element called hdrtonemap, " +
	"which only its Windows build includes), so HDR files stream as HDR."

func sectionView(sec settingsSection, saveURL string) views.SettingsSection {
	v := views.SettingsSection{ID: sec.ID, Title: sec.Title, Icon: sec.Icon, Intro: sec.Intro, SaveURL: saveURL, Values: map[string]any{}}
	for _, f := range sec.Fields {
		field := views.SettingField{Key: f.Key, Label: f.Label, Help: f.Help, Kind: string(f.Kind), Unit: f.Unit, Min: f.Min, Max: f.Max}
		for _, c := range f.Choices {
			field.Choices = append(field.Choices, views.SettingChoice{Value: fmt.Sprint(c.Value), Label: c.Label})
		}
		v.Fields = append(v.Fields, field)
	}
	return v
}

// shownValue is a stored setting as the form shows it.
func shownValue(sets torrserver.Fields, f settingField) any {
	switch f.Kind {
	case kindBool:
		return sets.Bool(f.Key) != f.Invert
	case kindInt:
		return sets.Int64(f.Key) / max(f.Scale, 1)
	case kindSelect:
		var v any
		_ = json.Unmarshal(sets[f.Key], &v)
		return fmt.Sprint(v)
	default:
		return sets.String(f.Key)
	}
}

// parse turns a posted form value into what TorrServer stores.
func (f settingField) parse(posted any) (any, error) {
	switch f.Kind {
	case kindBool:
		b, ok := posted.(bool)
		if !ok {
			return nil, errors.New("must be on or off")
		}
		return b != f.Invert, nil
	case kindInt:
		var n int64
		switch v := posted.(type) {
		case float64:
			n = int64(v)
		case string:
			parsed, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				return nil, errors.New("must be a whole number")
			}
			n = parsed
		default:
			return nil, errors.New("must be a whole number")
		}
		if n < f.Min || n > f.Max {
			return nil, fmt.Errorf("must be between %d and %d %s", f.Min, f.Max, f.Unit)
		}
		return n * max(f.Scale, 1), nil
	case kindSelect:
		for _, c := range f.Choices {
			if fmt.Sprint(c.Value) == fmt.Sprint(posted) {
				return c.Value, nil
			}
		}
		return nil, errors.New("is not one of the choices")
	default:
		text, ok := posted.(string)
		if !ok {
			return nil, errors.New("must be text")
		}
		text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
		if f.Check != nil {
			if err := f.Check(text); err != nil {
				return nil, err
			}
		}
		return text, nil
	}
}

// readSection reads the posted values of one section, keyed by field.
func readSection(r *http.Request, id string) (map[string]any, error) {
	var sig map[string]map[string]any
	if err := datastar.ReadSignals(r, &sig); err != nil {
		return nil, err
	}
	return sig[id], nil
}

func (s *Server) handleSaveEngineSettings(w http.ResponseWriter, r *http.Request) {
	if s.adminAPI(w, r) == nil {
		return
	}
	sec, ok := sectionByID(r.PathValue("section"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	posted, err := readSection(r, sec.ID)
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	status := func(st views.SourceStatus) {
		patchSource(w, r, views.SettingsForm(s.engineSectionView(ctx, sec), st), nil)
	}

	// Check everything before changing anything.
	engineChanges := map[string]any{}
	startup := s.store.State().TorrServer.Startup
	before := startup
	for _, f := range sec.Fields {
		value, present := posted[f.Key]
		if !present || (f.Startup && !s.managed()) {
			continue
		}
		stored, err := f.parse(value)
		if err != nil {
			status(failed("%s %v.", f.Label, err))
			return
		}
		if f.Startup {
			setStartup(&startup, f.Key, stored)
			continue
		}
		engineChanges[f.Key] = stored
	}

	client := s.torrServer.Client()
	current, err := client.Settings(ctx)
	if err != nil {
		status(failed("%s", engineAsleep))
		return
	}
	for key, value := range engineChanges {
		if raw, _ := json.Marshal(value); string(raw) == string(current[key]) {
			delete(engineChanges, key) // unchanged: no need to reconnect for it
		}
	}
	var done []string
	if len(engineChanges) > 0 {
		if err := client.UpdateSettings(ctx, engineChanges); err != nil {
			slog.Warn("saving engine settings failed", "section", sec.ID, "error", err)
			status(failed("TorrServer did not accept the settings: %v", err))
			return
		}
		done = append(done, "TorrServer reconnected")
	}
	if startup != before {
		if err := s.saveStartup(startup); err != nil {
			slog.Error("save engine startup options failed", "error", err)
			status(failed(saveFailed))
			return
		}
		if err := s.restartWithStartup(ctx, startup); err != nil {
			status(failed("Saved, but TorrServer did not restart: %v", err))
			return
		}
		done = append(done, "TorrServer restarted")
	}
	if len(done) == 0 {
		status(succeeded("Nothing changed."))
		return
	}
	status(succeeded("Saved. %s.", strings.Join(done, " and ")))
}

// saveStartup stores the managed engine's startup options.
func (s *Server) saveStartup(startup config.EngineStartup) error {
	return s.store.Update(func(st *config.State) error {
		st.TorrServer.Startup = startup
		return nil
	})
}

// restartWithStartup restarts the managed engine with new startup options.
func (s *Server) restartWithStartup(ctx context.Context, startup config.EngineStartup) error {
	s.engine.SetOptions(EngineOptions(startup))
	if err := s.engine.Restart(ctx); err != nil {
		return err
	}
	url, user, password := s.engine.Endpoint()
	return s.torrServer.SetEndpoint(url, user, password)
}

func startupValue(st config.EngineStartup, f settingField) any {
	switch f.Key {
	case "ProxyURL":
		return st.ProxyURL
	case "ProxyMode":
		if st.ProxyMode == "" {
			return "tracker"
		}
		return st.ProxyMode
	case "PublicIPv4":
		return st.PublicIPv4
	case "PublicIPv6":
		return st.PublicIPv6
	case "MaxSize":
		return st.MaxSize / max(f.Scale, 1)
	case "TorrentsDir":
		return st.TorrentsDir
	}
	return nil
}

func setStartup(st *config.EngineStartup, key string, v any) {
	switch key {
	case "ProxyURL":
		st.ProxyURL, _ = v.(string)
	case "ProxyMode":
		st.ProxyMode, _ = v.(string)
	case "PublicIPv4":
		st.PublicIPv4, _ = v.(string)
	case "PublicIPv6":
		st.PublicIPv6, _ = v.(string)
	case "MaxSize":
		st.MaxSize, _ = v.(int64)
	case "TorrentsDir":
		st.TorrentsDir, _ = v.(string)
	}
}

func (s *Server) handleSaveGStreamer(w http.ResponseWriter, r *http.Request) {
	if s.adminAPI(w, r) == nil {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	posted, err := readSection(r, gstreamerSection.ID)
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), settingsTimeout)
	defer cancel()
	status := func(st views.SourceStatus) {
		patchSource(w, r, views.SettingsForm(s.gstreamerView(ctx), st), nil)
	}
	changes := map[string]any{}
	for _, f := range gstreamerSection.Fields {
		value, present := posted[f.Key]
		if !present {
			continue
		}
		stored, err := f.parse(value)
		if err != nil {
			status(failed("%s %v.", f.Label, err))
			return
		}
		changes[f.Key] = stored
	}
	if err := s.torrServer.Client().UpdateGSTSettings(ctx, changes); err != nil {
		slog.Warn("saving GStreamer settings failed", "error", err)
		status(failed("TorrServer did not accept the GStreamer settings: %v", err))
		return
	}
	status(succeeded("Saved. New streams use these settings."))
}

func (s *Server) handleResetGStreamer(w http.ResponseWriter, r *http.Request) {
	if s.adminAPI(w, r) == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), settingsTimeout)
	defer cancel()
	st := succeeded("GStreamer settings restored to TorrServer's defaults.")
	if err := s.torrServer.Client().ResetGSTSettings(ctx); err != nil {
		st = failed("TorrServer did not reset the GStreamer settings: %v", err)
	}
	patchSource(w, r, views.SettingsForm(s.gstreamerView(ctx), st), nil)
}

// handleCancelLinks replaces the link secret: every shared link stops working.
func (s *Server) handleCancelLinks(w http.ResponseWriter, r *http.Request) {
	if s.adminAPI(w, r) == nil {
		return
	}
	st := succeeded("All shared links are cancelled. Links copied from now on work.")
	if err := s.store.Update(func(state *config.State) error {
		state.LinkSecret = ""
		return nil
	}); err != nil {
		st = failed(saveFailed)
	} else if signer, err := linkSigner(s.store); err != nil {
		st = failed("%v", err)
	} else {
		s.links.Store(signer)
	}
	patchSource(w, r, views.SecurityLinks(st), nil)
}
