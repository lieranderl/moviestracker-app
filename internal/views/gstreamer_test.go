package views_test

import (
	"html"
	"strings"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/views"
)

func gstCard(t *testing.T, v views.GStreamerSetup, compact bool) string {
	t.Helper()
	return html.UnescapeString(render(t, views.GStreamerCard(v, compact)))
}

func TestGStreamerCanBeInstalledFromThePageWithItsSize(t *testing.T) {
	out := gstCard(t, views.GStreamerSetup{Can: true, Admin: true, Pinned: "1.28.7", Size: "154 MB"}, false)
	for _, want := range []string{
		`id="gst-setup"`, "Install GStreamer (154 MB)", `@post('/api/gstreamer/install`,
		"MKV", "VLC", // why it helps, and what works without it
	} {
		if !strings.Contains(out, want) {
			t.Errorf("install offer lacks %q", want)
		}
	}
	member := gstCard(t, views.GStreamerSetup{Can: true, Pinned: "1.28.7", Size: "154 MB"}, false)
	if strings.Contains(member, "/api/gstreamer/install") || !strings.Contains(member, "administrator") {
		t.Error("only an administrator can install GStreamer; others are told to ask one")
	}
}

func TestGStreamerInstallShowsDownloadProgressThenInstalling(t *testing.T) {
	out := gstCard(t, views.GStreamerSetup{Can: true, Admin: true, Phase: "downloading", Percent: 56, Progress: "87 MB of 154 MB", Pinned: "1.28.7"}, true)
	for _, want := range []string{`<progress`, `value="56"`, `max="100"`, "87 MB of 154 MB", "56%"} {
		if !strings.Contains(out, want) {
			t.Errorf("download progress lacks %q", want)
		}
	}
	if strings.Contains(out, "/api/gstreamer/install") {
		t.Error("no install button while it downloads")
	}
	installing := gstCard(t, views.GStreamerSetup{Can: true, Admin: true, Phase: "installing", Pinned: "1.28.7"}, true)
	if !strings.Contains(installing, "Installing GStreamer") || strings.Contains(installing, `value="`) {
		t.Errorf("installing shows an indeterminate bar:\n%s", installing)
	}
}

func TestAFailedGStreamerInstallSaysWhyAndOffersARetry(t *testing.T) {
	out := gstCard(t, views.GStreamerSetup{Can: true, Admin: true, Phase: "failed", Error: "not enough free disk space", Pinned: "1.28.7", Size: "154 MB"}, true)
	if !strings.Contains(out, "not enough free disk space") || !strings.Contains(out, "Try again") {
		t.Errorf("failure card:\n%s", out)
	}
}

func TestAnOlderDownloadedGStreamerOffersAnUpdateButton(t *testing.T) {
	out := gstCard(t, views.GStreamerSetup{Can: true, Admin: true, Working: true, Version: "1.26.0", Installed: "1.26.0", Pinned: "1.28.7", Size: "154 MB"}, true)
	if !strings.Contains(out, "Update GStreamer to 1.28.7") || !strings.Contains(out, "/api/gstreamer/install") {
		t.Errorf("update offer:\n%s", out)
	}
}

func TestWorkingGStreamerIsOnlyShownOnTheSourcesCard(t *testing.T) {
	ready := views.GStreamerSetup{Can: true, Admin: true, Working: true, Version: "1.28.7", Installed: "1.28.7", Pinned: "1.28.7"}
	if out := gstCard(t, ready, false); !strings.Contains(out, "1.28.7") || strings.Contains(out, "/api/gstreamer/install") {
		t.Errorf("Sources card for a working GStreamer:\n%s", out)
	}
	if out := gstCard(t, ready, true); strings.Contains(out, "GStreamer") {
		t.Errorf("TorrServer page and dashboard need no card when all is well:\n%s", out)
	}
	if out := gstCard(t, views.GStreamerSetup{}, false); strings.Contains(out, "Install") {
		t.Errorf("without the macOS app there is nothing to install here:\n%s", out)
	}
}

func TestTheInstallShowsItsStepsUntilTorrServerRunsWithGStreamer(t *testing.T) {
	for phase, done := range map[string]int{"downloading": 1, "installing": 2, "starting": 3, "done": 4} {
		out := gstCard(t, views.GStreamerSetup{Can: true, Admin: true, Phase: phase, Pinned: "1.28.7", Size: "146 MB"}, true)
		if got := strings.Count(out, "step step-primary"); got != done {
			t.Errorf("%s: %d steps marked, want %d", phase, got, done)
		}
		for _, step := range []string{"Download", "Install", "Start TorrServer", "Ready"} {
			if !strings.Contains(out, ">"+step+"<") {
				t.Errorf("%s: steps lack %q", phase, step)
			}
		}
	}
	starting := gstCard(t, views.GStreamerSetup{Can: true, Admin: true, Phase: "starting", Pinned: "1.28.7"}, true)
	if !strings.Contains(starting, "Starting TorrServer with GStreamer 1.28.7") {
		t.Errorf("starting state:\n%s", starting)
	}
}

func TestTheReadyMessageCanBeClosedButProgressCannot(t *testing.T) {
	done := gstCard(t, views.GStreamerSetup{Can: true, Phase: "done", Pinned: "1.28.7", Working: true, Version: "1.28.7"}, true)
	if !strings.Contains(done, `@post('/api/gstreamer/dismiss?compact=1')`) || !strings.Contains(done, `aria-label="Close"`) {
		t.Errorf("the ready message has no close button:\n%s", done)
	}
	for _, phase := range []string{"downloading", "installing", "starting", "failed"} {
		if out := gstCard(t, views.GStreamerSetup{Can: true, Phase: phase, Pinned: "1.28.7"}, true); strings.Contains(out, "/api/gstreamer/dismiss") {
			t.Errorf("the %s card can be closed", phase)
		}
	}
}
