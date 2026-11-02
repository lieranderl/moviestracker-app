// scripts/update-pins.sh moves the TorrServer and GStreamer pins to newer
// upstream releases; these tests run it against a fake upstream.

//go:build unix

package packaging_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// upstream is what TorrServer's GitHub releases and GStreamer's download
// server offer.
type upstream struct {
	tsTag       string
	tsPublished time.Time
	tsAssets    map[string]string // file name → GitHub's digest, "sha256:…"
	tsLicense   string

	gstVersions  []string // folders under data/pkg/osx
	gstPkg       string   // the runtime installer's content, for every version
	gstPublished time.Time
}

// current is upstream as scripts/torrserver.lock and install-gstreamer.sh
// pin it: MatriX.148 and GStreamer 1.28.8.
func current() upstream {
	return upstream{
		tsTag:       "MatriX.148",
		tsPublished: time.Date(2026, 10, 31, 11, 28, 48, 0, time.UTC),
		tsAssets: map[string]string{
			"TorrServer-gst-darwin-amd64":      "sha256:16dd1349cc6da05cf5803c6d6074641698c2889378fe24f32fdc34757c9a4fab",
			"TorrServer-gst-darwin-arm64":      "sha256:c2a027085ca3c0e50bd6f87701024293d5c306b676fdcea0f4d169688d36cda0",
			"TorrServer-gst-linux-amd64":       "sha256:8d389986ee54508cc69a0dc91cfd41f8df73e0f2777fdc18464cd07082e0eddc",
			"TorrServer-gst-linux-arm64":       "sha256:40563b627fe91c59af67c1e385115f4baf6271ba719ad0a08c1770f81c4f767e",
			"TorrServer-gst-windows-amd64.exe": "sha256:6344cb5896d6d1dfe54aaf879157de8c5c8ed6eb847dbb80d87b0ea193dcb2a5",
		},
		gstVersions:  []string{"1.26.9", "1.28.7", "1.28.8"},
		gstPublished: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
}

// serve starts the fake upstream.
func (u upstream) serve(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/YouROK/TorrServer/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		type asset struct {
			Name   string `json:"name"`
			Digest string `json:"digest"`
		}
		rel := struct {
			Tag        string    `json:"tag_name"`
			Published  time.Time `json:"published_at"`
			Prerelease bool      `json:"prerelease"`
			Assets     []asset   `json:"assets"`
		}{Tag: u.tsTag, Published: u.tsPublished}
		for name, digest := range u.tsAssets {
			rel.Assets = append(rel.Assets, asset{name, digest})
		}
		rel.Assets = append(rel.Assets, asset{"TorrServer-freebsd-amd64", "sha256:00"})
		_ = json.NewEncoder(w).Encode(rel)
	})
	mux.HandleFunc("GET /YouROK/TorrServer/{tag}/LICENSE", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("tag") != u.tsTag {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(u.tsLicense))
	})
	mux.HandleFunc("GET /data/pkg/osx/{$}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `<html><body><a href="?C=N;O=D">Name</a> <a href="/data/pkg/">Parent</a> <a href="iphone/">iphone/</a>`)
		for _, v := range u.gstVersions {
			_, _ = fmt.Fprintf(w, `<a href="%s/">%s/</a>`, v, v)
		}
		_, _ = fmt.Fprint(w, `</body></html>`)
	})
	mux.HandleFunc("/data/pkg/osx/{version}/{file}", func(w http.ResponseWriter, r *http.Request) {
		v := r.PathValue("version")
		pkg := "gstreamer-1.0-" + v + "-universal.pkg"
		switch r.PathValue("file") {
		case pkg:
			w.Header().Set("Last-Modified", u.gstPublished.Format(http.TimeFormat))
			w.Header().Set("Content-Length", fmt.Sprint(len(u.gstPkg)))
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(u.gstPkg))
			}
		case pkg + ".sha256sum":
			_, _ = fmt.Fprintf(w, "%s  %s\n", sha(u.gstPkg), pkg) // #nosec G705 -- a fake server for the test, answering text/plain
		default:
			http.NotFound(w, r)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func sha(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// pinnedRepo copies the files update-pins.sh changes into a new folder
// laid out like the repository.
func pinnedRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range []string{"scripts/torrserver.lock", "macos/install-gstreamer.sh"} {
		data, err := os.ReadFile(filepath.Join("..", f)) // #nosec G304 -- the repository's own files
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(root, filepath.Dir(f)), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, f), data, 0o600); err != nil { // #nosec G703 -- the test's own temporary folder
			t.Fatal(err)
		}
	}
	return root
}

// updatePins runs update-pins.sh on root against srv, on the day now.
func updatePins(t *testing.T, root string, srv *httptest.Server, now time.Time) (string, error) {
	t.Helper()
	cmd := exec.Command(filepath.Join("..", "scripts", "update-pins.sh")) // #nosec G204 -- the script under test
	cmd.Env = append(os.Environ(),
		"PINS_ROOT="+root,
		"PINS_GITHUB_API="+srv.URL,
		"PINS_GITHUB_RAW="+srv.URL,
		"PINS_GSTREAMER="+srv.URL+"/data/pkg/osx",
		"PINS_NOW="+fmt.Sprint(now.Unix()),
	)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- the test's own files
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestANewerTorrServerIsPinnedWithGitHubsChecksums(t *testing.T) {
	up := current()
	up.tsTag = "MatriX.149"
	up.tsPublished = time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	up.tsLicense = "GNU GENERAL PUBLIC LICENSE\nVersion 3\n"
	for name := range up.tsAssets {
		up.tsAssets[name] = "sha256:" + sha(name+" 149")
	}
	root := pinnedRepo(t)

	out, err := updatePins(t, root, up.serve(t), time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("update-pins.sh: %v\n%s", err, out)
	}

	want := `# TorrServer release Moviestracker is tested with, and the SHA-256 GitHub
# publishes for each file. Update all lines together.
version MatriX.149
TorrServer-gst-darwin-amd64 ` + sha("TorrServer-gst-darwin-amd64 149") + `
TorrServer-gst-darwin-arm64 ` + sha("TorrServer-gst-darwin-arm64 149") + `
TorrServer-gst-linux-amd64 ` + sha("TorrServer-gst-linux-amd64 149") + `
TorrServer-gst-linux-arm64 ` + sha("TorrServer-gst-linux-arm64 149") + `
TorrServer-gst-windows-amd64.exe ` + sha("TorrServer-gst-windows-amd64.exe 149") + `
# TorrServer's licence (GPL-3.0) at that tag, shipped next to its binary.
LICENSE ` + sha("GNU GENERAL PUBLIC LICENSE\nVersion 3\n") + `
`
	if got := read(t, filepath.Join(root, "scripts", "torrserver.lock")); got != want {
		t.Errorf("torrserver.lock =\n%s\nwant\n%s", got, want)
	}
	if !strings.Contains(out, "TorrServer MatriX.148 → MatriX.149") {
		t.Errorf("the summary does not name the TorrServer update:\n%s", out)
	}
}

// unchanged fails t unless the pinned files in root are the repository's.
func unchanged(t *testing.T, root string) {
	t.Helper()
	for _, f := range []string{"scripts/torrserver.lock", "macos/install-gstreamer.sh"} {
		if read(t, filepath.Join(root, f)) != read(t, filepath.Join("..", f)) {
			t.Errorf("%s changed", f)
		}
	}
}

func TestNothingChangesWhenThePinsAreCurrent(t *testing.T) {
	root := pinnedRepo(t)

	out, err := updatePins(t, root, current().serve(t), time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("update-pins.sh: %v\n%s", err, out)
	}

	unchanged(t, root)
	if strings.TrimSpace(out) != "" {
		t.Errorf("update-pins.sh reported updates:\n%s", out)
	}
}

func TestAReleaseYoungerThanAWeekWaits(t *testing.T) {
	up := current()
	up.tsTag = "MatriX.149"
	up.tsPublished = time.Date(2026, 11, 9, 0, 0, 0, 0, time.UTC)
	root := pinnedRepo(t)

	out, err := updatePins(t, root, up.serve(t), time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("update-pins.sh: %v\n%s", err, out)
	}

	unchanged(t, root)
}

func TestATorrServerReleaseMissingAPlatformIsRefused(t *testing.T) {
	up := current()
	up.tsTag = "MatriX.149"
	up.tsPublished = time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	delete(up.tsAssets, "TorrServer-gst-windows-amd64.exe")
	root := pinnedRepo(t)

	out, err := updatePins(t, root, up.serve(t), time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC))

	if err == nil || !strings.Contains(out, "TorrServer-gst-windows-amd64.exe") {
		t.Errorf("update-pins.sh = %v, want a failure naming the missing file:\n%s", err, out)
	}
	unchanged(t, root)
	if _, err := os.Stat(filepath.Join(root, "scripts", "torrserver.lock.next")); !os.IsNotExist(err) {
		t.Error("a half-written lock was left behind")
	}
}

func TestTheNewestStableGStreamerIsPinnedAndDevelopmentVersionsSkipped(t *testing.T) {
	up := current()
	up.gstVersions = []string{"1.26.9", "1.28.7", "1.28.10", "1.28.8", "1.29.2"}
	up.gstPkg = "GStreamer 1.28.10 runtime"
	root := pinnedRepo(t)

	out, err := updatePins(t, root, up.serve(t), time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("update-pins.sh: %v\n%s", err, out)
	}

	script := read(t, filepath.Join(root, "macos", "install-gstreamer.sh"))
	for _, want := range []string{
		`gst_version="1.28.10"`,
		`gst_sha256="` + sha("GStreamer 1.28.10 runtime") + `"`,
		`gst_size=25` + "\n",
		`gst_url="https://gstreamer.freedesktop.org/data/pkg/osx/$gst_version/gstreamer-1.0-$gst_version-universal.pkg"`,
	} {
		if !strings.Contains(script, want) {
			t.Errorf("install-gstreamer.sh lacks %s", want)
		}
	}
	if !strings.Contains(out, "GStreamer 1.28.8 → 1.28.10") {
		t.Errorf("the summary does not name the GStreamer update:\n%s", out)
	}
}

func TestAnOlderTorrServerMarkedLatestIsNotADowngrade(t *testing.T) {
	up := current()
	up.tsTag = "MatriX.145"
	up.tsPublished = time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	root := pinnedRepo(t)

	out, err := updatePins(t, root, up.serve(t), time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("update-pins.sh: %v\n%s", err, out)
	}

	unchanged(t, root)
}
