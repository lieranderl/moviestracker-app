package tray_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/tray"
)

func TestTheMenuOffersANewReleaseOnlyWhenThereIsOne(t *testing.T) {
	rel := &tray.Release{Version: "v0.4.0", Download: "https://github.com/lieranderl/moviestracker-app/releases/download/v0.4.0/Moviestracker-Setup-v0.4.0-x64.exe"}

	got := find(t, tray.Menu(tray.MenuState{Version: "v0.3.2", Release: rel}), tray.NewRelease)
	if got.Hidden || got.Disabled || got.Title != "Download Moviestracker v0.4.0…" {
		t.Errorf("new release item = %+v", got)
	}

	if got := find(t, tray.Menu(tray.MenuState{Version: "v0.4.0"}), tray.NewRelease); !got.Hidden {
		t.Errorf("without a new release the item shows: %+v", got)
	}
}

func TestTheTrayLearnsAboutANewReleaseFromTheServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/update" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"version":"v0.4.0","notes":"https://github.com/lieranderl/moviestracker-app/releases/tag/v0.4.0","download":"https://github.com/lieranderl/moviestracker-app/releases/download/v0.4.0/Moviestracker-Setup-v0.4.0-x64.exe"}`))
	}))
	t.Cleanup(srv.Close)

	got := tray.LatestRelease(context.Background(), srv.URL)

	want := tray.Release{
		Version:  "v0.4.0",
		Notes:    "https://github.com/lieranderl/moviestracker-app/releases/tag/v0.4.0",
		Download: "https://github.com/lieranderl/moviestracker-app/releases/download/v0.4.0/Moviestracker-Setup-v0.4.0-x64.exe",
	}
	if got == nil || *got != want {
		t.Errorf("LatestRelease() = %+v, want %+v", got, want)
	}
}

func TestTheTraySeesNoReleaseWhenTheServerHasNone(t *testing.T) {
	for name, answer := range map[string]http.HandlerFunc{
		"no new release": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) },
		"still setting up": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
		},
		"garbage": func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>")) },
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(answer)
			t.Cleanup(srv.Close)

			if got := tray.LatestRelease(context.Background(), srv.URL); got != nil {
				t.Errorf("LatestRelease() = %+v, want none", got)
			}
		})
	}
}

func TestTheTrayOpensOnlyGitHubLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"v0.4.0","notes":"https://github.com/lieranderl/moviestracker-app/releases/tag/v0.4.0","download":"C:\\Windows\\System32\\cmd.exe"}`))
	}))
	t.Cleanup(srv.Close)

	if got := tray.LatestRelease(context.Background(), srv.URL); got != nil {
		t.Errorf("LatestRelease() = %+v, want a release pointing elsewhere than GitHub refused", got)
	}
}
