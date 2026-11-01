package torrserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

// viewedServer is a TorrServer (MatriX.142+) keeping viewed files with
// their timecodes, as its /viewed API does with TrackTimecode on.
func viewedServer(t *testing.T) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	kept := map[string]map[int]float64{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Action    string  `json:"action"`
			Hash      string  `json:"hash"`
			FileIndex int     `json:"file_index"`
			TimeCode  float64 `json:"timecode"`
		}
		if r.URL.Path != "/viewed" || json.NewDecoder(r.Body).Decode(&req) != nil {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch req.Action {
		case "set":
			if kept[req.Hash] == nil {
				kept[req.Hash] = map[int]float64{}
			}
			kept[req.Hash][req.FileIndex] = req.TimeCode
		case "list":
			list := []map[string]any{}
			for i, tc := range kept[req.Hash] {
				list = append(list, map[string]any{"hash": req.Hash, "file_index": i, "timecode": tc})
			}
			_ = json.NewEncoder(w).Encode(list)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestTorrServerKeepsWhereAFileWasLeft(t *testing.T) {
	ts := viewedServer(t)
	client := torrserver.NewClient(ts.URL, ts.Client())
	ctx := context.Background()
	const hash = "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111"
	if err := client.SetViewed(ctx, hash, 2, 2530); err != nil {
		t.Fatal(err)
	}
	got, err := client.Timecode(ctx, hash, 2)
	if err != nil || got != 2530*time.Second {
		t.Errorf("Timecode() = %v, %v; want 2530 seconds", got, err)
	}
	if got, _ := client.Timecode(ctx, hash, 1); got != 0 {
		t.Errorf("an unplayed file's Timecode() = %v, want 0", got)
	}
}
