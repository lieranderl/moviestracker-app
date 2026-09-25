package tmdb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// ErrInvalidKey reports a key TMDB refuses (HTTP 401).
var ErrInvalidKey = errors.New("tmdb: the API key was rejected")

// CheckKey asks TMDB whether the client's key works, using the cheap
// /3/configuration endpoint. It returns ErrInvalidKey for a refused key and
// another error when TMDB cannot be reached.
func (c *Client) CheckKey(ctx context.Context) error {
	req, err := c.newRequest(ctx, "/3/configuration")
	if err != nil {
		return err
	}
	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("reach tmdb: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return ErrInvalidKey
	case resp.StatusCode != http.StatusOK:
		return fmt.Errorf("tmdb returned status %d", resp.StatusCode)
	}
	return nil
}
