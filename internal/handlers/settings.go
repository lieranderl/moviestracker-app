package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/i18n"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"

	"github.com/a-h/templ"
	"github.com/starfederation/datastar-go/datastar"
)

const (
	settingsTimeout     = 10 * time.Second
	reconnectCost       = "Saving makes TorrServer reconnect (about 2 seconds)."
	startupOnly         = "Only when Moviestracker runs TorrServer (Settings → Sources)."
	reachableNeedsHTTPS = "Reachable from other devices needs HTTPS."
	engineAsleep        = "TorrServer is not answering, so its settings cannot be shown. Check Settings → Sources."
	gstCost             = "Applied at once to new streams."
	httpsCost           = "TorrServer reads these when it starts with --ssl, so restart it to apply them."
	gstNotBuilt         = "This TorrServer was built without GStreamer, so MKV files cannot be converted for the browser. " +
		"The TorrServer Moviestracker runs (make torrserver) is the GStreamer build."
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
		templ.Handler(views.UsersPage(user, s.usersView(r.Context(), user))).ServeHTTP(w, r)
	default:
		sec, ok := sectionByID(id)
		if !ok {
			http.NotFound(w, r)
			return
		}
		templ.Handler(views.SettingsPage(user, s.engineSectionView(ctx, r, sec))).ServeHTTP(w, r)
	}
}

// managed reports whether Moviestracker runs the engine (startup options apply).
func (s *Server) managed() bool {
	return s.engine != nil && s.engineMode() == config.EngineManaged
}

// engineSectionView shows a section with TorrServer's current values.
func (s *Server) engineSectionView(ctx context.Context, r *http.Request, sec settingsSection) views.SettingsSection {
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
	if sec.ID == "https" {
		ssl := sslStatus(ctx, s.torrServer.Client())
		v.Header = views.HTTPSCard(s.httpsView(r, sets, ssl, views.SourceStatus{}))
		if ssl != nil {
			// The card sets these, with TorrServer checking the pair first.
			v.Fields = slices.DeleteFunc(v.Fields, func(f views.SettingField) bool { return f.Key == "SslCert" || f.Key == "SslKey" })
		}
	}
	return v
}

// gstreamerView shows TorrServer's GStreamer settings.
func (s *Server) gstreamerView(ctx context.Context) views.SettingsSection {
	v := sectionView(gstreamerSection, "/api/settings/gstreamer")
	v.Cost, v.ResetURL = gstCost, "/api/settings/gstreamer/reset"
	client := s.torrServer.Client()
	gst, err := client.GSTSettings(ctx)
	switch {
	case err != nil:
		v.Unavailable = engineAsleep
		return v
	case !gst.BuiltIn:
		v.Unavailable = gstNotBuilt
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

// outOfRange is a number outside a setting's range.
type outOfRange struct {
	min, max int64
	unit     string
}

func (e outOfRange) Error() string {
	return strings.TrimSpace(fmt.Sprintf("must be between %d and %d %s", e.min, e.max, e.unit))
}

// fieldProblem says, in the request's language, why f's value was refused.
func fieldProblem(ctx context.Context, f settingField, err error) views.SourceStatus {
	reason := i18n.T(ctx, err.Error())
	if r, ok := errors.AsType[outOfRange](err); ok {
		reason = strings.TrimSpace(i18n.Tf(ctx, "must be between %d and %d %s", r.min, r.max, i18n.T(ctx, r.unit)))
	}
	return failed("%s %s.", i18n.T(ctx, f.Label), reason)
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
			return nil, outOfRange{f.Min, f.Max, f.Unit}
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
		patchSource(w, r, views.SettingsForm(s.engineSectionView(ctx, r, sec), st), nil)
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
			status(fieldProblem(ctx, f, err))
			return
		}
		if f.Startup {
			setStartup(&startup, f.Key, stored)
			continue
		}
		engineChanges[f.Key] = stored
	}
	if startup.Reachable && !startup.HTTPS {
		status(failed(reachableNeedsHTTPS))
		return
	}

	client := s.torrServer.Client()
	current, err := client.Settings(ctx)
	if err != nil {
		status(failed(engineAsleep))
		return
	}
	previous := map[string]any{} // what the changed settings held, to undo them
	for key, value := range engineChanges {
		if raw, _ := json.Marshal(value); string(raw) == string(current[key]) {
			delete(engineChanges, key) // unchanged: no need to reconnect for it
			continue
		}
		previous[key] = json.RawMessage(current[key])
	}
	// TorrServer reads some settings only when it starts.
	restart := startup != before
	for _, f := range sec.Fields {
		if _, changed := engineChanges[f.Key]; changed && f.AtStart && startup.HTTPS && s.managed() {
			restart = true
		}
	}
	reconnected, restarted := false, false
	if len(engineChanges) > 0 {
		if err := client.UpdateSettings(ctx, engineChanges); err != nil {
			slog.Warn("saving engine settings failed", "section", sec.ID, "error", err)
			status(failed("TorrServer did not accept the settings: %v", err))
			return
		}
		reconnected = true
	}
	if restart {
		if err := s.saveStartup(startup); err != nil {
			slog.Error("save engine startup options failed", "error", err)
			status(failed(saveFailed))
			return
		}
		if err := s.restartWithStartup(ctx, startup); err != nil {
			status(s.recoverEngine(ctx, before, previous, err))
			return
		}
		restarted = true
	}
	switch {
	case reconnected && restarted:
		status(succeeded("Saved. TorrServer reconnected and TorrServer restarted."))
	case reconnected:
		status(succeeded("Saved. TorrServer reconnected."))
	case restarted:
		status(succeeded("Saved. TorrServer restarted."))
	default:
		status(succeeded("Nothing changed."))
	}
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

// recoverEngine brings the managed engine back after it did not start with
// new settings (startErr): it starts without HTTPS, which TorrServer may be
// unable to serve, puts back the engine settings that were changed and the
// startup options that were in use, and starts with them again.
func (s *Server) recoverEngine(ctx context.Context, before config.EngineStartup, previous map[string]any, startErr error) views.SourceStatus {
	slog.Warn("engine did not restart with new settings; restoring the previous ones", "error", startErr)
	safe := before
	safe.HTTPS, safe.Reachable = false, false
	err := s.restartWithStartup(ctx, safe)
	if err == nil && len(previous) > 0 {
		err = s.torrServer.Client().UpdateSettings(ctx, previous)
	}
	if err == nil {
		err = s.saveStartup(before)
	}
	if err == nil && before != safe {
		err = s.restartWithStartup(ctx, before)
	}
	if err != nil {
		slog.Error("restoring the engine's previous settings failed", "error", err)
		return failed("Saved, but TorrServer did not restart: %v", startErr)
	}
	return failed("TorrServer did not start with these settings, so the previous ones are back: %v", startErr)
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
	case "HTTPS":
		return st.HTTPS
	case "Reachable":
		return st.Reachable
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
	case "HTTPS":
		st.HTTPS, _ = v.(bool)
	case "Reachable":
		st.Reachable, _ = v.(bool)
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
			status(fieldProblem(ctx, f, err))
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
