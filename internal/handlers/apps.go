package handlers

import (
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/gateway"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// openAppsPort opens the gateway's port at startup when an admin left it on.
func (s *Server) openAppsPort() {
	if s.appsPort == nil || !s.store.State().Gateway.Enabled {
		return
	}
	if err := s.appsPort.Open(); err != nil {
		problem := err.Error()
		s.appsProblem.Store(&problem)
		slog.Error("TorrServer for other apps is on, but its port did not open", "error", err)
		return
	}
	slog.Info("TorrServer is open to other apps", "port", s.appsPort.Number())
}

// appsView describes Other apps for the page opened with r.
func (s *Server) appsView(r *http.Request) views.AppsView {
	st := s.store.State().Gateway
	v := views.AppsView{
		On:       st.Enabled && s.appsPort.Addr() != "",
		Internet: st.Internet,
		Address:  s.appsAddress(r),
	}
	if p := s.appsProblem.Load(); p != nil && st.Enabled && !v.On {
		v.Problem = *p
	}
	for _, l := range st.Logins {
		v.Logins = append(v.Logins, views.AppLogin{Name: l.Name, User: l.User, CreatedAt: l.CreatedAt})
	}
	return v
}

// appsAddress is where apps reach the gateway: this machine as TVs see it
// (as for copied links), on the gateway's port.
func (s *Server) appsAddress(r *http.Request) string {
	origin, err := url.Parse(s.linkOrigin(r))
	if err != nil {
		return ""
	}
	return "http://" + net.JoinHostPort(origin.Hostname(), s.appsPort.Number())
}

// appsAction reads an Other apps action's signals: nil when the request was
// answered already (not an admin, no gateway, bad signals).
func (s *Server) appsAction(w http.ResponseWriter, r *http.Request, signals any) bool {
	if s.appsPort == nil {
		http.NotFound(w, r)
		return false
	}
	return s.sourceAction(w, r, "", signals)
}

// handleSwitchApps turns the gateway on or off, and the internet with it.
func (s *Server) handleSwitchApps(w http.ResponseWriter, r *http.Request) {
	var sig struct {
		On       bool `json:"appsOn"`
		Internet bool `json:"appsInternet"`
	}
	if !s.appsAction(w, r, &sig) {
		return
	}
	patch := func(st views.SourceStatus) {
		v := s.appsView(r)
		patchSource(w, r, views.AppsSwitch(v, st), map[string]any{"appsOn": v.On, "appsInternet": v.Internet})
	}
	if sig.On {
		if err := s.appsPort.Open(); err != nil {
			patch(failed("%v", err))
			return
		}
	} else if err := s.appsPort.Shut(); err != nil {
		slog.Warn("shut the port for other apps", "error", err)
	}
	s.appsProblem.Store(nil)
	if err := s.store.Update(func(st *config.State) error {
		st.Gateway.Enabled, st.Gateway.Internet = sig.On, sig.Internet
		return nil
	}); err != nil {
		slog.Error("save other apps", "error", err)
		patch(failed(saveFailed))
		return
	}
	switch {
	case !sig.On:
		patch(succeeded("TorrServer is closed to other apps."))
	case sig.Internet:
		patch(succeeded("TorrServer is open to other apps with a login, from anywhere."))
	default:
		patch(succeeded("TorrServer is open to other apps with a login, on your home network."))
	}
}

// handleAddAppLogin makes a login for a new app and shows its password once.
func (s *Server) handleAddAppLogin(w http.ResponseWriter, r *http.Request) {
	var sig struct {
		Name string `json:"appName"`
	}
	if !s.appsAction(w, r, &sig) {
		return
	}
	var login config.AppLogin
	var password string
	err := s.store.Update(func(st *config.State) error {
		var err error
		login, password, err = gateway.NewLogin(sig.Name, st.Gateway.Logins)
		if err != nil {
			return err
		}
		st.Gateway.Logins = append(st.Gateway.Logins, login)
		return nil
	})
	if err != nil {
		patchSource(w, r, views.AppsLogins(s.appsView(r), nil, failed("%v", err)), nil)
		return
	}
	created := &views.NewAppLogin{Name: login.Name, User: login.User, Password: password}
	patchSource(w, r, views.AppsLogins(s.appsView(r), created, views.SourceStatus{}), map[string]any{"appName": ""})
}

// handleRevokeAppLogin removes an app's login: it is signed out at once.
func (s *Server) handleRevokeAppLogin(w http.ResponseWriter, r *http.Request) {
	var sig struct{}
	if !s.appsAction(w, r, &sig) {
		return
	}
	user := r.PathValue("user")
	var name string
	err := s.store.Update(func(st *config.State) error {
		st.Gateway.Logins = slices.DeleteFunc(st.Gateway.Logins, func(l config.AppLogin) bool {
			if l.User == user {
				name = l.Name
				return true
			}
			return false
		})
		return nil
	})
	st := succeeded("%s can no longer use TorrServer.", name)
	switch {
	case err != nil:
		st = failed(saveFailed)
	case name == "":
		st = failed("There is no such login.")
	}
	patchSource(w, r, views.AppsLogins(s.appsView(r), nil, st), nil)
}
