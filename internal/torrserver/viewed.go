package torrserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// viewedRequest is a request to TorrServer's /viewed API (MatriX.142+
// keeps a timecode per file when its TrackTimecode setting is on).
type viewedRequest struct {
	Action    string  `json:"action"`
	Hash      string  `json:"hash"`
	FileIndex int     `json:"file_index,omitempty"`
	TimeCode  float64 `json:"timecode,omitempty"`
}

func (c *Client) viewed(ctx context.Context, req viewedRequest, out any) error {
	body, _ := json.Marshal(req)
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/viewed", bytes.NewReader(body))
	if err != nil {
		return err
	}
	r.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(r)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("torrserver viewed returned status %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return decodeJSON(resp.Body, out)
}

// SetViewed marks a file viewed up to seconds in, so TorrServer's other
// players can resume it there. TorrServer keeps 0 when its TrackTimecode
// setting is off.
func (c *Client) SetViewed(ctx context.Context, hash string, index int, seconds float64) error {
	return c.viewed(ctx, viewedRequest{Action: "set", Hash: hash, FileIndex: index, TimeCode: seconds}, nil)
}

// Timecode is where TorrServer says a file was left, in seconds; 0 when it
// keeps none.
func (c *Client) Timecode(ctx context.Context, hash string, index int) (time.Duration, error) {
	var list []viewedRequest
	if err := c.viewed(ctx, viewedRequest{Action: "list", Hash: hash}, &list); err != nil {
		return 0, err
	}
	for _, v := range list {
		if v.FileIndex == index && v.TimeCode > 0 {
			return time.Duration(v.TimeCode) * time.Second, nil
		}
	}
	return 0, nil
}
