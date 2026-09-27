package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/auth"
	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/gstinstall"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"

	"github.com/starfederation/datastar-go/datastar"
)

// topicGStreamer is the GStreamer download, while a page shows it.
const topicGStreamer = "gstreamer"

// gstCan reports whether Moviestracker can download GStreamer: in the macOS
// app, for the TorrServer it runs itself.
func (s *Server) gstCan() bool {
	return s.gst != nil && s.engine != nil && s.engineMode() == config.EngineManaged
}

// gstSetup is the GStreamer card for user, with echo telling whether
// TorrServer's GStreamer works.
func (s *Server) gstSetup(user *auth.User, st gstinstall.Status, echo torrserver.EchoInfo) views.GStreamerSetup {
	v := views.GStreamerSetup{
		Can: s.gstCan(), Admin: user != nil && user.IsAdmin(),
		Phase: string(st.Phase), Error: st.Error, Installed: st.Installed, Pinned: st.Pinned,
		Size: size(st.Total), Working: echo.GSTAvailable, Version: echo.GSTVersion,
	}
	if st.Phase == gstinstall.Downloading && st.Total > 0 {
		v.Percent = int(st.Downloaded * 100 / st.Total)
		v.Progress = size(st.Downloaded) + " / " + size(st.Total)
	}
	return v
}

// gstStatus is the installer's status, or none without an installer.
func (s *Server) gstStatus() gstinstall.Status {
	if s.gst == nil {
		return gstinstall.Status{}
	}
	return s.gst.Status()
}

// gstNow builds the card from a fresh engine check, for a page render.
func (s *Server) gstNow(ctx context.Context, user *auth.User) views.GStreamerSetup {
	if !s.gstCan() {
		return views.GStreamerSetup{}
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	echo, _ := s.torrServer.Client().Echo(ctx)
	return s.gstSetup(user, s.gstStatus(), echo)
}

// handleGStreamerInstall starts the GStreamer download.
func (s *Server) handleGStreamerInstall(w http.ResponseWriter, r *http.Request) {
	user := s.adminAPI(w, r)
	if user == nil {
		return
	}
	if !s.gstCan() {
		http.Error(w, "Moviestracker cannot install GStreamer here.", http.StatusNotFound)
		return
	}
	s.gst.Start()
	v := s.gstNow(r.Context(), user)
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.GStreamerCard(v, r.URL.Query().Get("compact") == "1")); err != nil {
		logSSEError(r, "patch gstreamer", err)
	}
}

// handleGStreamerDismiss closes the "GStreamer is ready" message, for
// everyone: a finished install is forgotten, a running one goes on.
func (s *Server) handleGStreamerDismiss(w http.ResponseWriter, r *http.Request) {
	user := s.apiUser(w, r)
	if user == nil {
		return
	}
	if !s.gstCan() {
		http.Error(w, "Moviestracker cannot install GStreamer here.", http.StatusNotFound)
		return
	}
	s.gst.Dismiss()
	v := s.gstNow(r.Context(), user)
	if err := datastar.NewSSE(w, r).PatchElementTempl(views.GStreamerCard(v, r.URL.Query().Get("compact") == "1")); err != nil {
		logSSEError(r, "patch gstreamer", err)
	}
}

// handleGStreamerStream keeps the GStreamer card current while the page is
// open.
func (s *Server) handleGStreamerStream(w http.ResponseWriter, r *http.Request) {
	user := s.apiUser(w, r)
	if user == nil {
		return
	}
	if !s.acquireSSE() {
		w.Header().Set("Retry-After", "2")
		http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
		return
	}
	defer s.releaseSSE()
	compact := r.URL.Query().Get("compact") == "1"
	sse := datastar.NewSSE(w, r)
	ctx, cancel := s.streamContext(r)
	defer cancel()
	sub := s.live.Subscribe(topicGStreamer, topicEngine)
	defer sub.Close()
	var last string
	for {
		select {
		case <-ctx.Done():
			return
		case <-sub.C:
		}
		st := liveValue[gstinstall.Status](sub, topicGStreamer)
		echo := liveValue[torrserver.EchoInfo](sub, topicEngine)
		if err := patchIfChanged(ctx, sse, views.GStreamerCard(s.gstSetup(user, st, echo), compact), &last); err != nil {
			logSSEError(r, "patch gstreamer", err)
			return
		}
	}
}
