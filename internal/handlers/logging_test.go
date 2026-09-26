package handlers_test

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/handlers"
)

// A share link works for anyone holding it, so the log, which is easier to
// read than the settings file, must not hold one.
func TestTheRequestLogLeavesOutShareLinkTokens(t *testing.T) {
	const token = "dd8255ecdc7ca55fb0bbf81323d87062db1f6d1c.1.1790000000.c2lnbmF0dXJl" // #nosec G101 -- a made-up share link token
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	handler := handlers.RecoveryMiddleware(handlers.LoggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/panic") {
			panic("boom")
		}
	})))
	for _, path := range []string{"/s/" + token + "/Dune.mkv", "/s/" + token + "/hls/seg/3.m4s", "/s/" + token + "/panic"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}

	out := logged.String()
	if strings.Contains(out, token) || strings.Contains(out, "c2lnbmF0dXJl") {
		t.Errorf("the log holds a share link token:\n%s", out)
	}
	for _, want := range []string{"/s/…/Dune.mkv", "/s/…/hls/seg/3.m4s", "/s/…/panic"} {
		if !strings.Contains(out, want) {
			t.Errorf("the log lacks %q:\n%s", want, out)
		}
	}
}

// Docker asks /healthz every 30 seconds: passing probes would bury the log.
func TestPassingHealthProbesAreNotLogged(t *testing.T) {
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	status := http.StatusOK
	handler := handlers.LoggingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
	for _, path := range []string{"/healthz", "/readyz"} {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	if logged.Len() != 0 {
		t.Errorf("passing health probes were logged:\n%s", logged.String())
	}
	status = http.StatusServiceUnavailable
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if !strings.Contains(logged.String(), "path=/readyz") {
		t.Errorf("a failing probe was not logged:\n%s", logged.String())
	}
}

// A path can carry encoded line breaks: the log shows them encoded again, as
// they were sent, so no request can start a log line of its own.
func TestLineBreaksInPathsCannotForgeLogLines(t *testing.T) {
	var logged bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logged, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	handler := handlers.LoggingMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/movie%0D%0Alevel=ERROR", nil))

	if out := logged.String(); !strings.Contains(out, "/movie%0D%0Alevel=ERROR") || strings.Count(out, "\n") != 1 {
		t.Errorf("the log shows the path as:\n%s", out)
	}
}
