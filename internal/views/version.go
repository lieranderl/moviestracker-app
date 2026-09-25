package views

import "sync/atomic"

var appVersion atomic.Value // string

// SetVersion sets the Moviestracker version the footer shows; the server
// sets it once at start.
func SetVersion(v string) { appVersion.Store(v) }

// versionLabel is "Moviestracker v0.1.30", or "" when no version is set.
func versionLabel() string {
	v, _ := appVersion.Load().(string)
	switch v {
	case "":
		return ""
	case "dev":
		return "Moviestracker development build"
	}
	return "Moviestracker " + v
}
