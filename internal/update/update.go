// Package update tells whether a newer Moviestracker has been released: it
// asks GitHub for the latest stable release and picks the download for this
// platform, so the web pages and the tray and menu bar apps can say so.
// Nothing is downloaded or installed.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Repo is where Moviestracker is released.
const Repo = "lieranderl/moviestracker-app"

// Platform is how this Moviestracker was installed, which decides the
// download a newer release offers.
type Platform string

// Platforms.
const (
	Mac     Platform = "mac"     // the DMG
	Windows Platform = "windows" // the installer
	Docker  Platform = "docker"  // the image: no download, a docker pull
)

// Release is a newer Moviestracker.
type Release struct {
	Version  string // its tag, "v1.2.3"
	Notes    string // its page on GitHub, with the release notes
	Download string // the file for this platform; "" when there is none
}

// Checker asks GitHub about new releases. It is safe for concurrent use.
type Checker struct {
	current  string
	platform Platform
	api      string
	client   *http.Client
	now      func() time.Time
	enabled  func() bool

	mu     sync.Mutex
	latest *Release  // nil until a newer release is seen
	etag   string    // GitHub's tag for the last answer, so an unchanged one is not sent again
	last   time.Time // when GitHub last answered
}

// Every is how often GitHub is asked.
const Every = 24 * time.Hour

// Option configures a Checker.
type Option func(*Checker)

// WithAPI replaces GitHub's API address, for tests.
func WithAPI(url string) Option {
	return func(c *Checker) { c.api = strings.TrimRight(url, "/") }
}

// WithClock replaces the wall clock, for tests.
func WithClock(now func() time.Time) Option {
	return func(c *Checker) { c.now = now }
}

// WithEnabled makes checks depend on enabled, the administrator's setting:
// while it is false, GitHub is not asked and no release is available.
func WithEnabled(enabled func() bool) Option {
	return func(c *Checker) { c.enabled = enabled }
}

// New returns a checker for the running version on platform.
func New(current string, platform Platform, opts ...Option) *Checker {
	c := &Checker{
		current:  current,
		platform: platform,
		api:      "https://api.github.com",
		client:   &http.Client{Timeout: 20 * time.Second},
		now:      time.Now,
		enabled:  func() bool { return true },
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Current is the running version.
func (c *Checker) Current() string { return c.current }

// Platform is how this Moviestracker was installed.
func (c *Checker) Platform() Platform { return c.platform }

// Enabled reports whether the administrator lets Moviestracker ask GitHub.
func (c *Checker) Enabled() bool { return c.enabled() }

// Available returns the newer release, if one has been seen.
func (c *Checker) Available() (Release, bool) {
	if !c.enabled() {
		return Release{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.latest == nil {
		return Release{}, false
	}
	return *c.latest, true
}

// githubRelease is the part of GitHub's release object Moviestracker reads.
type githubRelease struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// Run checks for releases until ctx ends: now, and then once a day. A
// failed check is tried again every hour.
func (c *Checker) Run(ctx context.Context) {
	tick := time.NewTicker(time.Hour)
	defer tick.Stop()
	for {
		c.RefreshIfDue(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// RefreshIfDue asks GitHub unless it answered less than a day ago.
func (c *Checker) RefreshIfDue(ctx context.Context) {
	c.mu.Lock()
	due := c.last.IsZero() || c.now().Sub(c.last) >= Every
	c.mu.Unlock()
	if !due {
		return
	}
	if err := c.Refresh(ctx); err != nil {
		slog.Warn("could not check for a new Moviestracker release", "error", err)
	}
}

// Refresh asks GitHub for the latest release. Development builds, which
// have no version, never ask, nor does anything while checks are off.
func (c *Checker) Refresh(ctx context.Context) error {
	if _, _, ok := parse(c.current); !ok || !c.enabled() {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.api+"/repos/"+Repo+"/releases/latest", nil)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	c.mu.Lock()
	if c.etag != "" {
		req.Header.Set("If-None-Match", c.etag)
	}
	c.mu.Unlock()
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("ask GitHub for the latest release: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotModified {
		c.mu.Lock()
		c.last = c.now()
		c.mu.Unlock()
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ask GitHub for the latest release: %s", resp.Status)
	}
	var gr githubRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&gr); err != nil {
		return fmt.Errorf("read the latest release: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.etag = resp.Header.Get("ETag")
	c.last = c.now()
	c.latest = nil
	if newer(gr.Tag, c.current) {
		// Pages link to these, so only GitHub's own addresses are kept; the
		// release's page is known from its tag anyway.
		rel := Release{Version: gr.Tag, Notes: "https://github.com/" + Repo + "/releases/tag/" + gr.Tag}
		for _, a := range gr.Assets {
			if c.platform.wants(a.Name) && strings.HasPrefix(a.URL, "https://github.com/"+Repo+"/releases/download/") {
				rel.Download = a.URL
				break
			}
		}
		c.latest = &rel
	}
	return nil
}

// wants reports whether a release file named name is this platform's.
func (p Platform) wants(name string) bool {
	switch p {
	case Mac:
		return strings.HasPrefix(name, "Moviestracker-") && strings.HasSuffix(name, ".dmg")
	case Windows:
		return strings.HasPrefix(name, "Moviestracker-Setup-") && strings.HasSuffix(name, "-x64.exe")
	}
	return false
}

// newer reports whether the stable release latest comes after the running
// version current, which may be a pre-release: v1.2.0 comes after
// v1.2.0-rc.1. A pre-release latest, or anything unreadable, is never newer.
func newer(latest, current string) bool {
	vl, pre, okL := parse(latest)
	vc, currentPre, okC := parse(current)
	if !okL || !okC || pre {
		return false
	}
	for i := range vl {
		if vl[i] != vc[i] {
			return vl[i] > vc[i]
		}
	}
	return currentPre
}

// parse reads "vMAJOR.MINOR.PATCH", with pre telling whether a pre-release
// suffix follows ("v1.2.0-rc.1"); other forms are refused.
func parse(v string) (n [3]int, pre bool, ok bool) {
	core, suffix, pre := strings.Cut(v, "-")
	if pre && suffix == "" {
		return n, false, false
	}
	parts := strings.Split(strings.TrimPrefix(core, "v"), ".")
	if !strings.HasPrefix(core, "v") || len(parts) != 3 {
		return n, false, false
	}
	for i, p := range parts {
		x, err := strconv.Atoi(p)
		if err != nil || x < 0 || p != strconv.Itoa(x) {
			return n, false, false
		}
		n[i] = x
	}
	return n, pre, true
}
