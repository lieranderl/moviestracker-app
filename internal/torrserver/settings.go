package torrserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
)

// ErrNoGStreamer reports a TorrServer built without GStreamer.
var ErrNoGStreamer = errors.New("this TorrServer has no GStreamer")

// Fields is a TorrServer settings object as raw JSON fields. Keeping the
// raw form means a save sends back every field, including ones this client
// does not know (TorrServer replaces the whole object on save).
type Fields map[string]json.RawMessage

// Int returns an integer field, 0 when absent.
func (f Fields) Int(key string) int {
	var n float64
	_ = json.Unmarshal(f[key], &n)
	return int(n)
}

// Int64 returns an integer field that may exceed int32 (byte sizes).
func (f Fields) Int64(key string) int64 {
	var n float64
	_ = json.Unmarshal(f[key], &n)
	return int64(n)
}

// Bool returns a boolean field, false when absent.
func (f Fields) Bool(key string) bool {
	var b bool
	_ = json.Unmarshal(f[key], &b)
	return b
}

// String returns a string field, "" when absent.
func (f Fields) String(key string) string {
	var s string
	_ = json.Unmarshal(f[key], &s)
	return s
}

// with returns a copy of f with patch applied.
func (f Fields) with(patch map[string]any) (Fields, error) {
	out := make(Fields, len(f)+len(patch))
	for k, v := range f {
		out[k] = v
	}
	for k, v := range patch {
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("encode %s: %w", k, err)
		}
		out[k] = raw
	}
	return out, nil
}

// Settings reads TorrServer's engine settings (BTSets).
func (c *Client) Settings(ctx context.Context) (Fields, error) {
	var sets Fields
	if err := c.postJSON(ctx, "/settings", map[string]any{"action": "get"}, &sets); err != nil {
		return nil, err
	}
	return sets, nil
}

// UpdateSettings changes the given engine settings and keeps every other
// field. TorrServer then drops its torrents and reconnects (~2s).
func (c *Client) UpdateSettings(ctx context.Context, patch map[string]any) error {
	current, err := c.Settings(ctx)
	if err != nil {
		return err
	}
	next, err := current.with(patch)
	if err != nil {
		return err
	}
	if err := c.postJSON(ctx, "/settings", map[string]any{"action": "set", "sets": next}, nil); err != nil {
		return err
	}
	c.cacheSize.Store(nil) // the next CacheSize reads the new value
	return nil
}

// GSTSettings are TorrServer's GStreamer settings.
type GSTSettings struct {
	BuiltIn  bool   `json:"built_in"`
	Config   Fields `json:"config"`
	Defaults Fields `json:"defaults"`
}

// GSTSettings reads the GStreamer settings; BuiltIn is false for a
// TorrServer built without GStreamer.
func (c *Client) GSTSettings(ctx context.Context) (GSTSettings, error) {
	var gst GSTSettings
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/gst/settings", nil)
	if err != nil {
		return gst, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return gst, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return gst, fmt.Errorf("gst settings returned status %d", resp.StatusCode)
	}
	return gst, decodeJSON(resp.Body, &gst)
}

// UpdateGSTSettings changes the given GStreamer settings and keeps the rest;
// TorrServer applies them at once.
func (c *Client) UpdateGSTSettings(ctx context.Context, patch map[string]any) error {
	gst, err := c.GSTSettings(ctx)
	if err != nil {
		return err
	}
	if !gst.BuiltIn {
		return ErrNoGStreamer
	}
	next, err := gst.Config.with(patch)
	if err != nil {
		return err
	}
	return c.postJSON(ctx, "/gst/settings", map[string]any{"action": "set", "config": next}, nil)
}

// UseAppGStreamer points TorrServer at dir, the GStreamer the macOS app
// downloaded under root, unless someone chose another GStreamer folder. It
// reports whether the setting changed: TorrServer loads GStreamer when it
// starts, so it needs a restart then.
func (c *Client) UseAppGStreamer(ctx context.Context, dir, root string) (bool, error) {
	gst, err := c.GSTSettings(ctx)
	if err != nil || !gst.BuiltIn {
		return false, err
	}
	current := filepath.Clean(gst.Config.String("GSTPath"))
	if current == filepath.Clean(dir) {
		return false, nil
	}
	if gst.Config.String("GSTPath") != "" && !strings.HasPrefix(current, filepath.Clean(root)+string(filepath.Separator)) {
		return false, nil
	}
	next, err := gst.Config.with(map[string]any{"GSTPath": dir})
	if err != nil {
		return false, err
	}
	return true, c.postJSON(ctx, "/gst/settings", map[string]any{"action": "set", "config": next}, nil)
}

// ResetGSTSettings restores TorrServer's GStreamer defaults for this platform.
func (c *Client) ResetGSTSettings(ctx context.Context) error {
	return c.postJSON(ctx, "/gst/settings", map[string]any{"action": "def"}, nil)
}

// postJSON sends body to path and decodes the answer into out (unless nil).
func (c *Client) postJSON(ctx context.Context, path string, body any, out any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("torrserver %s returned status %d: %s", path, resp.StatusCode, bytes.TrimSpace(msg))
	}
	if out == nil {
		return nil
	}
	return decodeJSON(resp.Body, out)
}
