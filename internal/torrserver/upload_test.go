package torrserver_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

// An uploaded .torrent reaches TorrServer byte for byte, with its name, its
// title and the request to keep it.
func TestAnUploadedTorrentReachesTorrServerIntact(t *testing.T) {
	file := []byte("d4:infod4:name1:a6:lengthi1e12:piece lengthi16384e6:pieces20:\x00\x01\x02aaaaaaaaaaaaaaaaaee")
	var got []byte
	var name, title, save string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/torrent/upload" {
			http.NotFound(w, r)
			return
		}
		f, fh, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		defer func() { _ = f.Close() }()
		got, _ = io.ReadAll(f)
		name, title, save = fh.Filename, r.FormValue("title"), r.FormValue("save")
	}))
	defer ts.Close()

	err := torrserver.NewClient(ts.URL, ts.Client()).UploadTorrent(context.Background(), "show.torrent", file, "Show (2026)")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, file) || name != "show.torrent" || title != "Show (2026)" || save != "true" {
		t.Errorf("TorrServer got %q named %q, title %q, save %q", got, name, title, save)
	}
}
