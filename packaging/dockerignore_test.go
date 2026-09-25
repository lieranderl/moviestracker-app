package packaging_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The Dockerfile copies the whole source folder into its build stage, so
// the shared TMDB key, local data and releases must never be sent along.
func TestDockerBuildsNeverSeeSecretsOrLocalData(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), ".dockerignore"))
	if err != nil {
		t.Fatal(err)
	}
	var patterns []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "!") {
			patterns = append(patterns, strings.TrimSuffix(line, "/"))
		}
	}
	ignored := func(path string) bool {
		for _, p := range patterns {
			for dir := path; dir != "."; dir = filepath.Dir(dir) {
				if ok, _ := filepath.Match(p, dir); ok {
					return true
				}
			}
		}
		return false
	}
	for _, path := range []string{
		".tmdb-shared-key",
		".env",
		".env.local",
		".devdata/state.json",
		"dist/Moviestracker-v1.0.0.dmg",
		".worktrees/feature/.tmdb-shared-key",
		"bin/server",
		"installTorrServerMac.sh",
		"moviestracker_v1.0.0_linux_amd64/moviestracker",
		".claude/settings.local.json",
	} {
		if !ignored(path) {
			t.Errorf(".dockerignore lets %s into the build", path)
		}
	}
	for _, path := range []string{"go.mod", "cmd/server/main.go", "internal/views/layout.templ", "static/datastar.js", "frontend/app.css"} {
		if ignored(path) {
			t.Errorf(".dockerignore leaves out %s, which the build needs", path)
		}
	}
}
