package handlers

import (
	"context"
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/gateway"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// letReachableDevicesInThroughApps moves an install that opened TorrServer
// itself to other devices (before the engine stayed on loopback) to Other
// apps, where they now come in.
func (s *Server) letReachableDevicesInThroughApps() {
	if !s.store.State().TorrServer.Startup.Reachable {
		return
	}
	if err := s.store.Update(func(st *config.State) error {
		st.TorrServer.Startup.Reachable = false
		st.Gateway.Enabled = st.Gateway.Enabled || s.appsPort != nil
		return nil
	}); err != nil {
		slog.Error("move devices that reached TorrServer to Other apps", "error", err)
		return
	}
	slog.Info("TorrServer stays on this computer now; other devices come in through Settings → Other apps")
}

// openAppsPort opens the gateway's ports at startup when an admin left it on.
func (s *Server) openAppsPort() {
	if s.appsPort == nil || !s.store.State().Gateway.Enabled {
		return
	}
	if err := s.openAppsPorts(); err != nil {
		problem := err.Error()
		s.appsProblem.Store(&problem)
		slog.Error("TorrServer for other apps is on, but its port did not open", "error", err)
		return
	}
	slog.Info("TorrServer is open to other apps", "port", s.appsPort.Number(), "https_port", s.appsHTTPSPort())
}

// openAppsPorts opens the gateway's HTTP port, then its HTTPS one; HTTPS
// failing to open leaves HTTP open (appsTLSProblem says why).
func (s *Server) openAppsPorts() error {
	if err := s.appsPort.Open(); err != nil {
		return err
	}
	s.appsTLSProblem.Store(nil)
	if s.appsTLSPort == nil {
		return nil
	}
	if err := s.appsTLSPort.Open(); err != nil {
		problem := err.Error()
		s.appsTLSProblem.Store(&problem)
		slog.Error("TorrServer for other apps is open, but its HTTPS port did not open", "error", err)
	}
	return nil
}

// shutAppsPorts shuts both of the gateway's ports.
func (s *Server) shutAppsPorts() {
	for _, p := range []*gateway.Port{s.appsPort, s.appsTLSPort} {
		if p == nil {
			continue
		}
		if err := p.Shut(); err != nil {
			slog.Warn("shut the port for other apps", "error", err)
		}
	}
}

// appsHTTPSPort is the gateway's HTTPS port number, or "" while it is shut.
func (s *Server) appsHTTPSPort() string {
	if s.appsTLSPort == nil || s.appsTLSPort.Addr() == "" {
		return ""
	}
	return s.appsTLSPort.Number()
}

// appsView describes Other apps for the page opened with r.
func (s *Server) appsView(r *http.Request) views.AppsView {
	st := s.store.State().Gateway
	v := views.AppsView{
		On:       st.Enabled && s.appsPort.Addr() != "",
		Internet: st.Internet,
		Address:  s.appsAddress(r, "http", s.appsPort.Number()),
	}
	if p := s.appsProblem.Load(); p != nil && st.Enabled && !v.On {
		v.Problem = *p
	}
	if v.On {
		if p := s.appsTLSProblem.Load(); p != nil {
			v.HTTPSProblem = *p
		} else if port := s.appsHTTPSPort(); port != "" {
			switch s.torrServerCertificate(r.Context()) {
			case certificateHere:
				v.HTTPSAddress = s.appsAddress(r, "https", port)
			case certificateElsewhere:
				v.CertificateElsewhere = true
			}
		}
	}
	for _, l := range st.Logins {
		v.Logins = append(v.Logins, views.AppLogin{Name: l.Name, User: l.User, CreatedAt: l.CreatedAt})
	}
	return v
}

// appsAddress is where apps reach the gateway: this machine as TVs see it
// (as for copied links), on one of the gateway's ports.
func (s *Server) appsAddress(r *http.Request, scheme, port string) string {
	origin, err := url.Parse(s.linkOrigin(r))
	if err != nil {
		return ""
	}
	return scheme + "://" + net.JoinHostPort(origin.Hostname(), port)
}

// certificatePlace is where TorrServer's HTTPS certificate is, for the
// gateway's HTTPS port to serve it too.
type certificatePlace int

const (
	certificateNone      certificatePlace = iota // TorrServer serves no HTTPS
	certificateHere                              // its files load on this machine
	certificateElsewhere                         // its files are on another machine (an external TorrServer)
)

// torrServerCertificate says where TorrServer's certificate is: the gateway
// serves it only from files this machine reads, since TorrServer never
// hands out the key.
func (s *Server) torrServerCertificate(ctx context.Context) certificatePlace {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	cert, key, err := s.torrServer.CertificateFiles(ctx)
	if err != nil {
		return certificateNone
	}
	if _, err := tls.LoadX509KeyPair(cert, key); err != nil {
		return certificateElsewhere
	}
	return certificateHere
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
		if err := s.openAppsPorts(); err != nil {
			patch(failed("%v", err))
			return
		}
	} else {
		s.shutAppsPorts()
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
