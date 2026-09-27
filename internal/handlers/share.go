package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/streamlink"
	playback "github.com/lieranderl/moviestracker-app/internal/streams"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/views"
)

// shareLinkTTL is how long a link given to an external player keeps working.
const shareLinkTTL = 7 * 24 * time.Hour

// linkSigner returns the signer of share links, creating the secret on first use.
func linkSigner(store *config.Store) (*streamlink.Signer, error) {
	secret := store.State().LinkSecret
	if secret == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return nil, fmt.Errorf("generate link secret: %w", err)
		}
		err := store.Update(func(st *config.State) error {
			if st.LinkSecret == "" {
				st.LinkSecret = hex.EncodeToString(b)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("save link secret: %w", err)
		}
		secret = store.State().LinkSecret
	}
	raw, err := hex.DecodeString(secret)
	if err != nil {
		return nil, fmt.Errorf("link secret is not hex: %w", err)
	}
	return streamlink.NewSigner(raw, shareLinkTTL), nil
}

// shareLink is the signed path an external player opens for one file.
func (s *Server) shareLink(hash string, f torrserver.FileStat) string {
	return "/s/" + s.links.Load().Token(hash, f.ID) + "/" + url.PathEscape(torrserver.StreamFileName(hash, f))
}

// hlsShareLink is the signed path of a file's GStreamer HLS master playlist.
func (s *Server) hlsShareLink(hash string, f torrserver.FileStat) string {
	return "/s/" + s.links.Load().Token(hash, f.ID) + "/hls/master.m3u8"
}

// streamLinks gives views both share links of each file.
func (s *Server) streamLinks() views.StreamLinks {
	return views.StreamLinks{Direct: s.shareLink, HLS: s.hlsShareLink, Page: s.titlePages()}
}

// engineLabel is how pages name the TorrServer in use. Its address stays in
// Settings → Sources: pages and links never reveal it.
func (s *Server) engineLabel() string {
	if s.engine != nil && s.engineMode() == config.EngineManaged {
		return "Run by Moviestracker"
	}
	return "Your TorrServer"
}

// sharedViewer names who plays through a shared link: no account.
const sharedViewer = "Shared link"

// handleShareLink streams what a signed link grants to anyone holding it:
// /s/<token>/<file name> plays the file, /s/<token>/hls/… serves its
// GStreamer HLS. Nothing else of the engine is reachable.
func (s *Server) handleShareLink(w http.ResponseWriter, r *http.Request) {
	token, rest, _ := strings.Cut(r.PathValue("rest"), "/")
	grant, err := s.links.Load().Verify(token)
	if err != nil {
		status := http.StatusNotFound
		if errors.Is(err, streamlink.ErrExpired) {
			status = http.StatusGone
		}
		http.Error(w, "This link is not valid (any more). Get a new one from Moviestracker.", status)
		return
	}
	if hlsPath, ok := strings.CutPrefix(rest, "hls/"); ok {
		if !safeHLSPath(hlsPath) {
			http.NotFound(w, r)
			return
		}
		query := r.URL.Query()
		if query.Has("index") || hlsPath == "master.m3u8" {
			query.Set("index", strconv.Itoa(grant.File)) // the file the link grants, whatever was asked
		}
		prefix := "/s/" + token + "/hls/"
		enginePrefix := "/gst/" + grant.Hash + "/"
		play := s.playRequest(r, sharedViewer)
		play.Hash, play.Kind = grant.Hash, playback.HLS
		play.File, play.Audio, play.Segment = playback.HLSPosition(hlsPath, query)
		s.proxyEngine(w, r, "gst/"+grant.Hash+"/"+hlsPath, query.Encode(), &play, func(line string) string {
			if after, ok := strings.CutPrefix(line, enginePrefix); ok {
				return prefix + after
			}
			return strings.ReplaceAll(line, `URI="`+enginePrefix, `URI="`+prefix)
		})
		return
	}
	query := url.Values{"link": {grant.Hash}, "index": {strconv.Itoa(grant.File)}}
	play := s.playRequest(r, sharedViewer)
	play.Hash, play.File = grant.Hash, grant.File
	s.proxyEngine(w, r, "stream/"+url.PathEscape(rest), query.Encode()+"&play", &play, nil)
}

// safeHLSPath accepts the file names of GStreamer HLS: playlists, init and
// segments, subtitles.
func safeHLSPath(p string) bool {
	if p == "" || strings.Contains(p, "..") || strings.ContainsAny(p, ":\\") {
		return false
	}
	return p == "master.m3u8" || p == "video.m3u8" || p == "init.mp4" ||
		strings.HasPrefix(p, "seg/") || strings.HasPrefix(p, "subs/")
}
