package engine

import (
	"os"
	"path/filepath"
	"strings"
)

// FindBinary returns the TorrServer program to run: MT_TORRSERVER_BIN when
// set (and "" if it does not exist, so a typo is not silently ignored), else
// "torrserver" next to the Moviestracker executable, else
// <dataDir>/engine/bin/torrserver. It returns "" when none exists.
func FindBinary(getenv func(string) string, executable, dataDir string) string {
	if custom := strings.TrimSpace(getenv("MT_TORRSERVER_BIN")); custom != "" {
		if isProgram(custom) {
			return absolute(custom)
		}
		return ""
	}
	for _, candidate := range []string{
		filepath.Join(filepath.Dir(executable), "torrserver"),
		filepath.Join(dataDir, "engine", "bin", "torrserver"),
	} {
		if isProgram(candidate) {
			return absolute(candidate)
		}
	}
	return ""
}

func isProgram(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// absolute makes path independent of the working directory, which the engine
// process does not share.
func absolute(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}
