// The Mac app's tests run its build scripts.

//go:build unix

package packaging_test

import (
	"os"
	"os/exec"
	"testing"
)

const version = "v0.0.0-test"

func run(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...) // #nosec G204 -- the tests' own fixed commands
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return string(out)
}
