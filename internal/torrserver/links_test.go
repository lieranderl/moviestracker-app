package torrserver_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/torrserver"
)

func TestStreamLinksEndWithTheFileName(t *testing.T) {
	if got := torrserver.StreamFileName("abc", torrserver.FileStat{ID: 2, Path: "Season 1/ep 1.mkv"}); got != "ep 1.mkv" {
		t.Errorf("StreamFileName() = %q, want %q", got, "ep 1.mkv")
	}
	if got := torrserver.StreamFileName("abc", torrserver.FileStat{ID: 1}); got != "abc" {
		t.Errorf("StreamFileName(no path) = %q, want the hash", got)
	}
}

func TestPlaylistsListEveryVideoFileThroughTheGivenLinks(t *testing.T) {
	files := []torrserver.FileStat{{ID: 1, Path: "sample.mkv"}, {ID: 2, Path: "movie.mkv"}, {ID: 3, Path: "sub.srt"}}
	playlist := torrserver.GenerateM3UPlaylist("Movie", files, func(f torrserver.FileStat) string {
		return fmt.Sprintf("http://mt:8095/s/tok%d/%s", f.ID, f.Path)
	})
	want := "#EXTM3U\n#EXTINF:-1,sample.mkv\nhttp://mt:8095/s/tok1/sample.mkv\n#EXTINF:-1,movie.mkv\nhttp://mt:8095/s/tok2/movie.mkv\n"
	if playlist != want {
		t.Fatalf("playlist =\n%s\nwant\n%s", playlist, want)
	}
}

func TestTheTorrServerAddressCanBeChangedButOnlyToAnHTTPAddress(t *testing.T) {
	mgr := torrserver.NewManager("http://a:8090/")
	if mgr.ActiveURL() != "http://a:8090" {
		t.Fatalf("ActiveURL() = %q, want http://a:8090", mgr.ActiveURL())
	}
	for _, bad := range []string{"", "nas:8090", "file:///etc/passwd", "javascript:alert(1)", "http://"} {
		if err := mgr.SetURL(bad); err == nil {
			t.Errorf("SetURL(%q) should fail", bad)
		}
	}
	if mgr.ActiveURL() != "http://a:8090" {
		t.Fatalf("address changed after invalid input: %q", mgr.ActiveURL())
	}
	if err := mgr.SetURL("http://nas.lan:8090"); err != nil || mgr.ActiveURL() != "http://nas.lan:8090" {
		t.Fatalf("SetURL(valid) = %v, active %q", err, mgr.ActiveURL())
	}
	if got := torrserver.NewManager("not a url").ActiveURL(); got != "http://127.0.0.1:8090" {
		t.Errorf("NewManager(invalid) = %q, want the local default", got)
	}
}

func TestAnEngineWithCredentialsIsReachedWithThem(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, ok := r.BasicAuth(); !ok || user != "mt" || pass != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("MatriX.145"))
	}))
	defer ts.Close()

	mgr := torrserver.NewManager("http://127.0.0.1:1")
	if err := mgr.SetEndpoint(ts.URL, "mt", "pw"); err != nil {
		t.Fatalf("SetEndpoint(): %v", err)
	}
	if echo, err := mgr.Client().Echo(context.Background()); err != nil || echo.Version != "MatriX.145" {
		t.Fatalf("Echo() = %+v, %v", echo, err)
	}
	if user, pass := mgr.Credentials(); user != "mt" || pass != "pw" {
		t.Errorf("Credentials() = %q, %q, want the stream proxy to get them too", user, pass)
	}
	if err := mgr.SetURL(ts.URL); err != nil {
		t.Fatalf("SetURL(): %v", err)
	}
	if user, _ := mgr.Credentials(); user != "" {
		t.Errorf("SetURL() kept credentials %q; an external TorrServer without auth needs none", user)
	}
}
