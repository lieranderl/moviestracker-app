// The tests read the pinned versions from the repository, so a pin update
// (scripts/update-pins.sh, or by hand) does not have to edit them.

//go:build unix

package packaging_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// pins is what scripts/torrserver.lock and macos/install-gstreamer.sh pin.
type pins struct {
	torrServer string // "MatriX.150"
	gstVersion string // "1.28.9"
	gstSize    string // the runtime installer's size in bytes
}

func pinned(t *testing.T) pins {
	t.Helper()
	lock := readRepo(t, "scripts/torrserver.lock")
	script := readRepo(t, "macos/install-gstreamer.sh")
	return pins{
		torrServer: match(t, lock, `(?m)^version (MatriX\.\d+)$`),
		gstVersion: match(t, script, `(?m)^gst_version="([0-9.]+)"$`),
		gstSize:    match(t, script, `(?m)^gst_size=(\d+)$`),
	}
}

// torrServerAfter is the MatriX release k after the pinned one.
func (p pins) torrServerAfter(t *testing.T, k int) string {
	t.Helper()
	n, err := strconv.Atoi(strings.TrimPrefix(p.torrServer, "MatriX."))
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("MatriX.%d", n+k)
}

// gstNext is a newer stable GStreamer (next patch) and gstDev a development
// one (odd minor) after the pinned version.
func (p pins) gstNext(t *testing.T) (next, dev string) {
	t.Helper()
	v := strings.Split(p.gstVersion, ".")
	if len(v) != 3 {
		t.Fatalf("GStreamer version %q is not major.minor.patch", p.gstVersion)
	}
	minor, err1 := strconv.Atoi(v[1])
	patch, err2 := strconv.Atoi(v[2])
	if err1 != nil || err2 != nil {
		t.Fatalf("GStreamer version %q is not numeric", p.gstVersion)
	}
	return fmt.Sprintf("%s.%d.%d", v[0], minor, patch+1), fmt.Sprintf("%s.%d.2", v[0], minor+1)
}

func readRepo(t *testing.T, f string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", f)) // #nosec G304 -- the repository's own files
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func match(t *testing.T, s, expr string) string {
	t.Helper()
	m := regexp.MustCompile(expr).FindStringSubmatch(s)
	if m == nil {
		t.Fatalf("no %s in the pinned files", expr)
	}
	return m[1]
}
