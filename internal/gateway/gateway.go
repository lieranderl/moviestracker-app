// Package gateway opens the TorrServer Moviestracker uses to other apps
// (TorrServe on a TV, Lampa) on a port of its own. Each app signs in with a
// login of its own; the gateway passes its requests on with Moviestracker's
// login, so TorrServer itself stays on loopback with a password nobody else
// knows.
package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/streams"
)

// Upstream is the TorrServer requests go to, and Moviestracker's login to it.
type Upstream struct {
	URL, User, Password string
}

// Config is what the gateway needs.
type Config struct {
	// Store holds the gateway's settings and the apps' logins.
	Store *config.Store
	// Upstream is the TorrServer Moviestracker uses now.
	Upstream func() Upstream
	// Plays, when set, follows what apps play, for the dashboard.
	Plays *streams.Tracker
}

// Gateway is the http.Handler other apps talk to.
type Gateway struct {
	cfg    Config
	proxy  *httputil.ReverseProxy
	client *http.Client

	mu      sync.Mutex
	saved   map[string]time.Time   // info hash → until when it counts as saved
	signed  map[string]signedIn    // user → the login it last signed in with
	seen    map[netip.Addr]seenApp // device → the app that last signed in from it
	guesses *guessLimiter
	// mark is sent with every request passed on: one that comes back is
	// the gateway's own, so TorrServer's address is the gateway.
	mark string
}

// signedIn is a login that passed bcrypt: the Authorization header that
// carried it and the hash it matched. Apps ask many times a second (a TV
// polls the torrent list), and bcrypt is slow on purpose, so a header seen
// before is taken as long as the login's hash has not changed.
type signedIn struct {
	header, hash string
}

// markHeader carries the gateway's mark.
const markHeader = "X-Moviestracker-Gateway"

// savedFor is how long a torrent found in TorrServer's list is taken as
// saved: a player asks for many pieces of the same file.
const savedFor = time.Minute

// New returns the gateway for cfg.
func New(cfg Config) *Gateway {
	g := &Gateway{cfg: cfg, client: &http.Client{Timeout: 10 * time.Second}, saved: map[string]time.Time{}, signed: map[string]signedIn{}, seen: map[netip.Addr]seenApp{}, guesses: newGuessLimiter(), mark: rand.Text()}
	g.proxy = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			up := cfg.Upstream()
			target, err := url.Parse(up.URL)
			if err != nil {
				return
			}
			pr.SetURL(target)
			pr.Out.Host = target.Host
			pr.Out.SetBasicAuth(up.User, up.Password)
			pr.Out.Header.Set(markHeader, g.mark)
		},
		// The gateway lets browser apps read its answers (ServeHTTP): one
		// Access-Control-Allow-Origin from TorrServer as well would make
		// the browser refuse them.
		ModifyResponse: func(resp *http.Response) error {
			resp.Header.Del("Access-Control-Allow-Origin")
			return nil
		},
		// Streams go out as TorrServer sends them, not buffered.
		FlushInterval: -1,
	}
	return g
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Browser apps (Lampa) are loaded from another address: let them read
	// every answer, the gateway's refusals too. Logins go in a header, not
	// cookies, so no page can use one it was not given.
	if r.Header.Get("Origin") != "" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get(markHeader)), []byte(g.mark)) == 1 {
		http.Error(w, "Moviestracker's TorrServer address is this port itself: start that TorrServer, or choose another in Settings → Sources.", http.StatusLoopDetected)
		return
	}
	addr, ok := clientAddr(r.RemoteAddr)
	if !ok || (!atHome(addr) && !g.cfg.Store.State().Gateway.Internet) {
		http.Error(w, "Moviestracker lets apps in from the home network only (Settings → Other apps).", http.StatusForbidden)
		return
	}
	// From the internet, logins and streams cross it encrypted only.
	if !atHome(addr) && r.TLS == nil {
		http.Error(w, "From the internet, use TorrServer's HTTPS address (Settings → Other apps).", http.StatusForbidden)
		return
	}
	if isPreflight(r) {
		allowPreflight(w, r)
		return
	}
	now := time.Now()
	if g.guesses.blocked(addr, now) {
		http.Error(w, "Too many wrong logins: try again in a minute.", http.StatusTooManyRequests)
		return
	}
	// Decide on the path TorrServer will see: "//shutdown" or "/x/../shutdown"
	// is "/shutdown".
	r.URL.Path, r.URL.RawPath = path.Clean("/"+r.URL.Path), ""
	name, signed := g.login(r)
	if !signed && !g.savedStream(r) {
		g.guesses.failed(addr, now)
		w.Header().Set("WWW-Authenticate", `Basic realm="Moviestracker TorrServer"`)
		http.Error(w, "Sign in with a login from Moviestracker (Settings → Other apps).", http.StatusUnauthorized)
		return
	}
	if reason := refused(r); reason != "" {
		http.Error(w, reason, http.StatusForbidden)
		return
	}
	if signed {
		g.signedInFrom(addr, name, now)
	}
	if play, ok := playOf(r); ok && g.cfg.Plays != nil {
		play.Client, play.Viewer = addr.String(), g.viewer(addr, name, now)
		c := &counted{ResponseWriter: w, plays: g.cfg.Plays, play: play}
		// Deferred: the proxy panics with http.ErrAbortHandler when the
		// player hangs up mid-stream, and the session must still end.
		defer c.done()
		g.proxy.ServeHTTP(c, r)
		return
	}
	g.proxy.ServeHTTP(w, r)
}

// isPreflight reports whether r is the check a browser makes before an
// app loaded from another address (Lampa) sends a login or a JSON body.
// Browsers never put a login on it.
func isPreflight(r *http.Request) bool {
	return r.Method == http.MethodOptions && r.Header.Get("Origin") != "" && r.Header.Get("Access-Control-Request-Method") != ""
}

// allowPreflight lets the browser send the request it asked about: the
// login is checked on that request itself.
func allowPreflight(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Access-Control-Allow-Methods", r.Header.Get("Access-Control-Request-Method"))
	if asked := r.Header.Get("Access-Control-Request-Headers"); asked != "" {
		h.Set("Access-Control-Allow-Headers", asked)
	}
	// Chrome asks before a page on the internet reaches a home address.
	if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
		h.Set("Access-Control-Allow-Private-Network", "true")
	}
	h.Set("Access-Control-Max-Age", "600")
	h.Add("Vary", "Origin")
	h.Add("Vary", "Access-Control-Request-Method")
	h.Add("Vary", "Access-Control-Request-Headers")
	w.WriteHeader(http.StatusNoContent)
}

const managedByMoviestracker = "Moviestracker manages TorrServer's settings: change them in Moviestracker."

// refused says why r is not for apps, or "" when it may go to TorrServer.
// Apps play, add and remove torrents; stopping TorrServer, changing its
// settings and removing every torrent at once are Moviestracker's.
func refused(r *http.Request) string {
	p := r.URL.Path
	switch {
	case p == "/shutdown" || strings.HasPrefix(p, "/shutdown/"):
		return "Moviestracker runs TorrServer: apps cannot stop it."
	case r.Method == http.MethodGet || r.Method == http.MethodHead:
		return ""
	}
	switch p {
	case "/settings":
		if a, _ := action(r); a != "get" {
			return managedByMoviestracker
		}
	case "/torrents":
		switch a, ok := action(r); {
		case !ok:
			return "The request is too large."
		case a == "wipe":
			return "Apps remove torrents one at a time."
		}
	case "/storage/settings", "/gst/settings", "/waf", "/torznab/test":
		return managedByMoviestracker
	}
	return ""
}

// maxActionBody bounds the JSON bodies whose action the gateway reads.
const maxActionBody = 1 << 20

// action is the lowercase "action" of r's JSON body ("" when it has none,
// "?" when it is not JSON); ok is false when the body is too large to
// read. The body stays readable for TorrServer.
func action(r *http.Request) (_ string, ok bool) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxActionBody+1))
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	if err != nil || len(body) > maxActionBody {
		return "", false
	}
	var req struct {
		Action string `json:"action"`
	}
	if json.Unmarshal(body, &req) != nil {
		return "?", true
	}
	return strings.ToLower(strings.TrimSpace(req.Action)), true
}

// login returns the name of the app whose login r carries.
func (g *Gateway) login(r *http.Request) (string, bool) {
	user, password, ok := r.BasicAuth()
	if !ok {
		return "", false
	}
	header := r.Header.Get("Authorization")
	for _, l := range g.cfg.Store.State().Gateway.Logins {
		if l.User != user {
			continue
		}
		g.mu.Lock()
		seen, ok := g.signed[user]
		g.mu.Unlock()
		if ok && seen.hash == l.PasswordHash && subtle.ConstantTimeCompare([]byte(seen.header), []byte(header)) == 1 {
			return l.Name, true
		}
		if bcrypt.CompareHashAndPassword([]byte(l.PasswordHash), []byte(password)) != nil {
			return "", false
		}
		g.mu.Lock()
		g.signed[user] = signedIn{header: header, hash: l.PasswordHash}
		g.mu.Unlock()
		return l.Name, true
	}
	return "", false
}

// savedStream reports whether r plays a torrent already in TorrServer's
// list: what a video player asks for with a link an app gave it.
func (g *Gateway) savedStream(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	hash, ok := streamedHash(r)
	if !ok {
		return false
	}
	now := time.Now()
	g.mu.Lock()
	until, known := g.saved[hash]
	g.mu.Unlock()
	if known && now.Before(until) {
		return true
	}
	if !g.inTorrServer(r.Context(), hash) {
		return false
	}
	g.mu.Lock()
	for h, until := range g.saved {
		if now.After(until) {
			delete(g.saved, h)
		}
	}
	g.saved[hash] = now.Add(savedFor)
	g.mu.Unlock()
	return true
}

// streamedHash is the one torrent r streams or lists: /stream?link=,
// /play/{hash}/{file} or /playlist?hash=.
func streamedHash(r *http.Request) (string, bool) {
	p, q := r.URL.Path, r.URL.Query()
	one := func(key string) (string, bool) {
		if v := q[key]; len(v) == 1 {
			return infoHash(v[0])
		}
		return "", false
	}
	switch {
	case p == "/stream" || strings.HasPrefix(p, "/stream/"):
		return one("link")
	case p == "/playlist" || strings.HasPrefix(p, "/playlist/"):
		return one("hash")
	case strings.HasPrefix(p, "/play/"):
		if parts := strings.Split(strings.TrimPrefix(p, "/play/"), "/"); len(parts) == 2 {
			return infoHash(parts[0])
		}
	}
	return "", false
}

// infoHash is the lowercase hex info hash link names: the hash itself, or
// a magnet link with exactly one.
func infoHash(link string) (string, bool) {
	link = strings.ToLower(strings.TrimSpace(link))
	if rest, ok := strings.CutPrefix(link, "magnet:?"); ok {
		q, err := url.ParseQuery(rest)
		if err != nil || len(q["xt"]) != 1 {
			return "", false
		}
		link, ok = strings.CutPrefix(q["xt"][0], "urn:btih:")
		if !ok {
			return "", false
		}
	}
	if len(link) != 40 || strings.Trim(link, "0123456789abcdef") != "" {
		return "", false
	}
	return link, true
}

// inTorrServer asks TorrServer whether hash is in its list.
func (g *Gateway) inTorrServer(ctx context.Context, hash string) bool {
	up := g.cfg.Upstream()
	body, _ := json.Marshal(map[string]string{"action": "get", "hash": hash})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(up.URL, "/")+"/torrents", bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(markHeader, g.mark)
	req.SetBasicAuth(up.User, up.Password)
	resp, err := g.client.Do(req)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// loginAlphabet leaves out 0/O and 1/I/L, which are easy to mistype on a
// TV remote.
const loginAlphabet = "23456789abcdefghjkmnpqrstuvwxyz"

// NewLogin makes a login for the app called name, with a user name unlike
// those in existing and a random password (shown once: only its hash is
// kept).
func NewLogin(name string, existing []config.AppLogin) (config.AppLogin, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return config.AppLogin{}, "", errors.New("give the app a name, such as Living room TV")
	}
	base := userName(name)
	user := base
	for n := 2; taken(user, existing); n++ {
		user = fmt.Sprintf("%s%d", base, n)
	}
	password, err := randomPassword()
	if err != nil {
		return config.AppLogin{}, "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return config.AppLogin{}, "", fmt.Errorf("keep the password: %w", err)
	}
	return config.AppLogin{Name: name, User: user, PasswordHash: string(hash), CreatedAt: time.Now().UTC()}, password, nil
}

// userName is name in lowercase letters and digits: "Living room TV" is
// "livingroomtv".
func userName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "app"
	}
	return b.String()
}

func taken(user string, logins []config.AppLogin) bool {
	for _, l := range logins {
		if l.User == user {
			return true
		}
	}
	return false
}

// randomPassword is three groups of four characters, about 59 random bits:
// short enough to type with a remote, and the gateway limits guessing.
func randomPassword() (string, error) {
	size := big.NewInt(int64(len(loginAlphabet)))
	b := make([]byte, 0, 14)
	for i := range 12 {
		if i > 0 && i%4 == 0 {
			b = append(b, '-')
		}
		n, err := rand.Int(rand.Reader, size)
		if err != nil {
			return "", fmt.Errorf("make a password: %w", err)
		}
		b = append(b, loginAlphabet[n.Int64()])
	}
	return string(b), nil
}
