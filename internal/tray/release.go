package tray

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// Release is a newer Moviestracker, as the server reports it.
type Release struct {
	Version  string `json:"version"`
	Notes    string `json:"notes"`    // its page on GitHub
	Download string `json:"download"` // the installer; "" when missing
}

// Page is where the menu item leads: the installer, or the release's page.
func (r Release) Page() string {
	if r.Download != "" {
		return r.Download
	}
	return r.Notes
}

var releaseClient = &http.Client{
	Timeout: 5 * time.Second,
	// Before setup every page redirects to it: that is no release.
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// LatestRelease asks the server at baseURL whether a newer Moviestracker is
// out; nil when there is none, or when the server cannot say.
func LatestRelease(ctx context.Context, baseURL string) *Release {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/update", nil)
	if err != nil {
		return nil
	}
	resp, err := releaseClient.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var rel Release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&rel); err != nil || rel.Version == "" ||
		!onGitHub(rel.Notes) || (rel.Download != "" && !onGitHub(rel.Download)) {
		return nil
	}
	return &rel
}

// onGitHub reports whether link is a page on GitHub: the menu opens it with
// the shell, which would run a program as readily as it opens a page.
func onGitHub(link string) bool {
	return strings.HasPrefix(link, "https://github.com/")
}
