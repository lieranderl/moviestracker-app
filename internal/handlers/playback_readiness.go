package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/starfederation/datastar-go/datastar"
)

// handlePlaybackReadiness refreshes connection capabilities without touching media.
func (s *Server) handlePlaybackReadiness(w http.ResponseWriter, r *http.Request) {
	if s.apiUser(w, r) == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	client := s.torrServer.Client()
	echo, _ := client.Echo(ctx)
	w.Header().Set("Cache-Control", "no-store")
	if client != s.torrServer.Client() {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := patchGSTSignal(datastar.NewSSE(w, r, func(_ *datastar.ServerSentEventGenerator) { w.Header().Set("Cache-Control", "no-store") }), echo); err != nil {
		logSSEError(r, "patch playback readiness", err)
	}
}
