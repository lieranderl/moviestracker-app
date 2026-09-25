package handlers_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/lieranderl/moviestracker-app/internal/config"
)

// addingEngine is a TorrServer that records what it is asked to add.
type addingEngine struct {
	mu      sync.Mutex
	links   []string          // action "add"
	titles  []string          // their titles
	uploads map[string]string // uploaded .torrent file name -> title
	saved   bool              // uploads asked to be kept
}

func newAddingEngine(t *testing.T) (*addingEngine, string) {
	t.Helper()
	e := &addingEngine{uploads: map[string]string{}}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		defer e.mu.Unlock()
		switch r.URL.Path {
		case "/echo":
			_, _ = io.WriteString(w, "MatriX.145")
		case "/torrents":
			var req map[string]any
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req["action"] == "add" {
				e.links = append(e.links, req["link"].(string))
				title, _ := req["title"].(string)
				e.titles = append(e.titles, title)
				_, _ = io.WriteString(w, `{"hash":"x"}`)
				return
			}
			_, _ = io.WriteString(w, `[]`)
		case "/torrent/upload":
			if err := r.ParseMultipartForm(1 << 20); err != nil { // #nosec G120 -- a test fake
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			e.saved = r.FormValue("save") != ""
			for _, fhs := range r.MultipartForm.File {
				for _, fh := range fhs {
					e.uploads[fh.Filename] = r.FormValue("title")
				}
			}
			_, _ = io.WriteString(w, `{"hash":"y"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	return e, ts.URL
}

// torrentFile is the start of a real .torrent: a bencoded dictionary.
const torrentFile = "d8:announce35:udp://tracker.example:1337/announce4:infod6:lengthi1e4:name1:a12:piece lengthi16384e6:pieces20:aaaaaaaaaaaaaaaaaaaaee"

// addForm posts the Add Torrents form as the browser sends it.
func addForm(t *testing.T, l *local, cookie *http.Cookie, links, title string, files map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("addLinks", links)
	_ = mw.WriteField("addTitle", title)
	for name, content := range files {
		fw, _ := mw.CreateFormFile("addFiles", name)
		_, _ = io.WriteString(fw, content)
	}
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/torrserver/add", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Datastar-Request", "true")
	return l.do(t, req, cookie)
}

func TestTorrentsCanBeAddedFromLinksAndTorrentFilesAtOnce(t *testing.T) {
	engine, url := newAddingEngine(t)
	l := newLocal(t, withAdmin(t), withEngineAt(url))
	links := strings.Join([]string{
		"magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=One",
		"https://tracker.example/download/two.torrent",
		"  http://tracker.example/three.torrent  ",
		"0123456789ABCDEF0123456789ABCDEF01234567",
	}, "\n")
	rr := addForm(t, l, l.admin(t), links, "", map[string]string{"four.torrent": torrentFile, "five.torrent": torrentFile})

	if len(engine.links) != 4 || engine.links[2] != "http://tracker.example/three.torrent" {
		t.Errorf("links added = %q, want the 4 links, trimmed", engine.links)
	}
	if len(engine.uploads) != 2 || !engine.saved {
		t.Errorf("uploads = %v (saved %v), want both .torrent files kept by TorrServer", engine.uploads, engine.saved)
	}
	if !strings.Contains(rr.Body.String(), "Added 6 torrents") {
		t.Errorf("response does not report 6 added:\n%s", rr.Body)
	}
}

func TestATitleNamesTheTorrentWhenOnlyOneIsAdded(t *testing.T) {
	engine, url := newAddingEngine(t)
	l := newLocal(t, withAdmin(t), withEngineAt(url))
	addForm(t, l, l.admin(t), "", "Inception (2010)", map[string]string{"inception.torrent": torrentFile})
	if engine.uploads["inception.torrent"] != "Inception (2010)" {
		t.Errorf("upload title = %q, want the custom title", engine.uploads["inception.torrent"])
	}
	addForm(t, l, l.admin(t), "magnet:?xt=urn:btih:aa\nmagnet:?xt=urn:btih:bb", "Ignored", nil)
	for _, title := range engine.titles {
		if title != "" {
			t.Errorf("with two torrents the title was used: %q", engine.titles)
		}
	}
}

func TestLinksAndFilesTorrServerMustNotOpenAreRefused(t *testing.T) {
	engine, url := newAddingEngine(t)
	l := newLocal(t, func(st *config.Store) {
		addAccount(t, st, "admin", config.RoleAdmin)
		addAccount(t, st, "anna", config.RoleViewer)
	}, withEngineAt(url))
	links := "file:///etc/passwd\nftp://example.com/x.torrent\njavascript:alert(1)\nnot a link"
	rr := addForm(t, l, l.signIn(t, l.accounts.Lookup("anna")), links, "", map[string]string{"notes.txt": "hello, not a torrent"})

	if len(engine.links) != 0 || len(engine.uploads) != 0 {
		t.Fatalf("forwarded to TorrServer: links %q, uploads %v", engine.links, engine.uploads)
	}
	body := rr.Body.String()
	for _, want := range []string{"file:///etc/passwd", "notes.txt", "Nothing was added"} {
		if !strings.Contains(body, want) {
			t.Errorf("response does not mention %q:\n%s", want, body)
		}
	}
}
