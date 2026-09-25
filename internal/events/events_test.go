package events_test

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/events"
)

func TestTheLatestWarningsAndErrorsAreKeptAndStillLogged(t *testing.T) {
	var out bytes.Buffer
	rec := events.NewRecorder(slog.NewTextHandler(&out, nil), 2)
	log := slog.New(rec).With("component", "engine")

	log.Info("http request", "path", "/movies") // not a problem
	log.Warn("TorrServer did not add a torrent link", "error", errors.New("status 500"))
	log.Error("TorrServer exited", "error", errors.New("signal: killed"))
	log.Error("save torrserver failed", "error", errors.New("disk full"))

	got := rec.Recent()
	if len(got) != 2 || got[0].Message != "save torrserver failed" || got[1].Message != "TorrServer exited" {
		t.Fatalf("recent = %+v, want the last two problems, newest first", got)
	}
	if got[0].Level != slog.LevelError || got[0].Detail != "disk full" || got[0].Time.IsZero() {
		t.Errorf("newest = %+v, want an error with its error text as detail", got[0])
	}
	if !strings.Contains(out.String(), "http request") || !strings.Contains(out.String(), "component=engine") {
		t.Errorf("records were not passed on to the log:\n%s", out.String())
	}
}
