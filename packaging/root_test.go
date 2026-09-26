// Package packaging_test checks the release scripts by running them: the Mac
// app and its DMG (with fake TorrServer binaries, offline), and the Docker
// build's context. scripts/ci/windows-install-e2e.ps1 and
// scripts/ci/docker-e2e.sh check the Windows installer and the image.
package packaging_test

import (
	"path/filepath"
	"testing"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}
