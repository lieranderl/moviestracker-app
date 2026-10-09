// Package enginetest lets a test binary double as a fake TorrServer, so the
// engine supervisor can be tested with a real child process.
package enginetest

import (
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// FakeEnv set to "1" in a test binary's environment makes it a fake TorrServer.
const FakeEnv = "ENGINE_FAKE_TORRSERVER"

// RunFakeIfRequested serves as a fake TorrServer and exits when FakeEnv is
// set; otherwise it returns at once. Call it first in TestMain.
func RunFakeIfRequested() {
	if os.Getenv(FakeEnv) != "1" {
		return
	}
	fakeTorrServer()
	os.Exit(0)
}

// fakeTorrServer accepts TorrServer's flags, enforces accs.db Basic auth like
// `TorrServer --httpauth`, and exits after FAKE_EXIT_AFTER when set.
func fakeTorrServer() {
	fs := flag.NewFlagSet("torrserver", flag.ExitOnError)
	ip := fs.String("ip", "", "")
	port := fs.String("port", "8090", "")
	dir := fs.String("path", ".", "")
	fs.String("logpath", "", "")
	httpAuth := fs.Bool("httpauth", false, "")
	sslOn := fs.Bool("ssl", false, "")
	for _, name := range []string{"proxyurl", "proxymode", "pubipv4", "pubipv6", "maxsize", "torrentsdir"} {
		fs.String(name, "", "")
	}
	_ = fs.Parse(os.Args[1:])

	accounts := map[string]string{}
	if *httpAuth {
		raw, err := os.ReadFile(filepath.Join(*dir, "accs.db")) // #nosec G304 -- test fake
		if err != nil || json.Unmarshal(raw, &accounts) != nil {
			fmt.Fprintln(os.Stderr, "fake: unreadable accs.db")
			os.Exit(3)
		}
	}
	authorized := func(r *http.Request) bool {
		user, pass, ok := r.BasicAuth()
		return !*httpAuth || (ok && accounts[user] == pass)
	}
	// FAKE_FAIL_STARTS=n makes the first n starts in a directory exit at
	// once, as a TorrServer that cannot start yet does.
	if n, err := strconv.Atoi(os.Getenv("FAKE_FAIL_STARTS")); err == nil {
		counter := filepath.Join(*dir, "fake-starts")
		raw, _ := os.ReadFile(counter) // #nosec G304 -- test fake
		started, _ := strconv.Atoi(string(raw))
		_ = os.WriteFile(counter, []byte(strconv.Itoa(started+1)), 0o600)
		if started < n {
			fmt.Fprintln(os.Stderr, "fake: failing this start")
			os.Exit(1)
		}
	}
	if after, err := time.ParseDuration(os.Getenv("FAKE_EXIT_AFTER")); err == nil {
		time.AfterFunc(after, func() { os.Exit(1) })
	}
	mux := http.NewServeMux()
	// /args reports the command line, so tests see the flags the engine got.
	mux.HandleFunc("GET /args", func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(os.Args[1:]) })
	mux.HandleFunc("GET /echo", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("MatriX.fake")) })
	// /settings keeps what was set across restarts, as TorrServer's BTSets.
	var setsMu sync.Mutex
	setsFile := filepath.Join(*dir, "fake-settings.json")
	sets := map[string]any{"CacheSize": 67108864}
	if raw, err := os.ReadFile(setsFile); err == nil { // #nosec G304 -- test fake
		_ = json.Unmarshal(raw, &sets)
	}
	saveSets := func() {
		raw, _ := json.Marshal(sets)
		_ = os.WriteFile(setsFile, raw, 0o600)
	}
	ssl := newFakeSSL(*sslOn, *dir, *port, func() map[string]any {
		setsMu.Lock()
		defer setsMu.Unlock()
		return maps.Clone(sets)
	}, func(cert, key string) {
		setsMu.Lock()
		defer setsMu.Unlock()
		sets["SslCert"], sets["SslKey"] = cert, key
		saveSets()
	}, authorized)
	if err := ssl.start(); err != nil {
		fmt.Fprintln(os.Stderr, "fake:", err)
		os.Exit(1)
	}
	ssl.routes(mux)
	// With --ssl, a certificate outside the engine folder (where uploads are
	// kept) stops the start, as a file TorrServer cannot read does. Only the
	// path's text is compared: the fake opens no path a request supplied.
	if cert, _ := sets["SslCert"].(string); *sslOn && cert != "" {
		root, _ := filepath.Abs(*dir)
		if !strings.HasPrefix(cert, root+string(filepath.Separator)) {
			fmt.Fprintln(os.Stderr, "fake: cannot start HTTPS with", cert)
			os.Exit(1)
		}
	}
	mux.HandleFunc("POST /settings", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var req struct {
			Action string         `json:"action"`
			Sets   map[string]any `json:"sets"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		setsMu.Lock()
		defer setsMu.Unlock()
		if req.Action == "set" {
			sets = req.Sets
			saveSets()
		}
		_ = json.NewEncoder(w).Encode(sets)
	})
	mux.HandleFunc("POST /torrents", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`[]`))
	})
	mux.HandleFunc("GET /shutdown", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		go func() { time.Sleep(20 * time.Millisecond); os.Exit(0) }()
	})
	srv := &http.Server{Addr: net.JoinHostPort(*ip, *port), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, "fake:", err)
		os.Exit(2)
	}
}
