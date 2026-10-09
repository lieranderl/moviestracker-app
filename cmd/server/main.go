package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/auth"
	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/engine"
	"github.com/lieranderl/moviestracker-app/internal/events"
	"github.com/lieranderl/moviestracker-app/internal/gateway"
	"github.com/lieranderl/moviestracker-app/internal/gstinstall"
	"github.com/lieranderl/moviestracker-app/internal/handlers"
	"github.com/lieranderl/moviestracker-app/internal/sources"
	"github.com/lieranderl/moviestracker-app/internal/streams"
	"github.com/lieranderl/moviestracker-app/internal/torrserver"
	"github.com/lieranderl/moviestracker-app/internal/update"
)

// version is set by release builds: -ldflags "-X main.version=v1.2.3".
var version = "dev"

// sharedTMDBKey is the TMDB key release builds carry (set with -ldflags by
// scripts/macapp.sh, scripts/winapp.sh and the Dockerfile), used when there is
// no other key.
var sharedTMDBKey string

// sharedJacRedKey is Moviestracker's jacred.su project key (unlimited
// searches), carried by release builds like sharedTMDBKey and used for
// jacred.su when there is no other key.
var sharedJacRedKey string

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		fmt.Println("moviestracker " + version)
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--health" {
		if err := checkHealth(listenAddr()); err != nil {
			fmt.Fprintln(os.Stderr, "unhealthy:", err)
			os.Exit(1)
		}
		return
	}
	// Initialize structured logging via slog
	// The recorder keeps the latest warnings and errors for the dashboard.
	problems := events.NewRecorder(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}), 50)
	logger := slog.New(problems)
	slog.SetDefault(logger)

	// Load local .env if present
	loadDotEnv()

	dataDir, err := resolveDataDir()
	if err != nil {
		slog.Error("no data directory", "error", err)
		os.Exit(1)
	}
	store, err := config.Open(dataDir)
	if err != nil {
		slog.Error("cannot open the data directory", "dir", dataDir, "error", err)
		os.Exit(1)
	}
	if err := checkWritable(dataDir); err != nil {
		slog.Error("cannot write to the data directory", "error", err)
		os.Exit(1)
	}
	slog.Info("data directory", "dir", dataDir)

	// Plain HTTP on the LAN is the default; turn Secure cookies and HSTS on
	// only behind TLS (a reverse proxy or your own certificate).
	secureCookies := envBool("MT_SECURE_COOKIES", false)
	accounts := auth.NewAccounts(store)
	sessions := auth.NewSessionManager(1024, rand.Reader, auth.WithStore(store, accounts.Lookup))

	env := config.EnvFrom(os.Getenv)
	env.SharedTMDBKey = sharedTMDBKey
	env.SharedJacRedKey = sharedJacRedKey
	effective := env.Apply(store.State())
	connector := sources.Connector{Health: sources.NewHealth()}
	clients := connector.Connect(effective.Sources)
	if clients.Catalog == nil {
		slog.Warn("no TMDB key yet; add one in Settings → Sources")
	}
	torrMgr := torrserver.NewManagerFor(effective.TorrServer)
	sup := newEngine(dataDir, torrMgr, store.State().TorrServer.Startup)
	if err := settleEngineMode(store, sup); err != nil {
		slog.Error("cannot save the engine mode", "error", err)
	}
	if managedEngine(env, store.State().TorrServer, sup) {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		if err := sup.Start(ctx); err != nil {
			slog.Error("TorrServer did not start; see Settings → Sources", "error", err)
		} else if dir, ok := gstinstall.InstalledIn(gstRoot(dataDir)); ok {
			useAppGStreamer(ctx, torrMgr.Client(), sup, dir, gstRoot(dataDir))
		}
		cancel()
	}
	slog.Info("sources", "jacred", effective.Sources.JacRedURL, "torrserver", torrMgr.ActiveURL())

	// Until the first admin exists, another device needs this code to create
	// it; a browser on this machine does not.
	var setupCode string
	if accounts.NeedsSetup() {
		if setupCode, err = newSetupCode(rand.Reader); err != nil {
			slog.Error("cannot make a setup code; set up from a browser on this machine", "error", err)
		} else {
			slog.Info("setup: open Moviestracker on this machine, or enter this setup code on another device", "setup_code", setupCode)
		}
	}

	// What plays, in Moviestracker and in other apps, for the dashboard.
	plays := streams.New(30 * time.Second)

	// Other apps (TorrServe, Lampa) reach TorrServer here once an admin
	// turns it on in Settings → Other apps; nothing listens until then.
	// Both ports serve the same gateway: HTTP, and HTTPS with the certificate
	// TorrServer serves (Settings → HTTPS).
	apps := gateway.New(gateway.Config{
		Store: store,
		Plays: plays,
		Upstream: func() gateway.Upstream {
			url, user, password := torrMgr.Endpoint()
			return gateway.Upstream{URL: url, User: user, Password: password}
		},
	})
	appsPort := gateway.NewPort(appsListenAddr(), apps)
	appsTLSPort := gateway.NewPort(appsTLSListenAddr(), apps).WithTLS(gateway.NewCertificates(torrMgr.CertificateFiles).Get)

	// Once a day, unless an admin turned it off, ask GitHub whether a newer
	// Moviestracker is out, to tell admins and the menu bar and tray apps.
	releases := update.New(version, platform(), update.WithEnabled(func() bool { return !store.State().Updates.Off }))
	releaseCtx, stopReleases := context.WithCancel(context.Background())
	defer stopReleases()
	go releases.Run(releaseCtx)

	server, err := handlers.NewServer(handlers.Config{
		Sessions:          sessions,
		Plays:             plays,
		Accounts:          accounts,
		Store:             store,
		Env:               env,
		Connector:         connector,
		SecureCookies:     secureCookies,
		MaxSSEStreams:     128,
		LoginAttempts:     10,
		LoginWindow:       time.Minute,
		LoginClientKeys:   4096,
		TrustedProxyCIDRs: envList("TRUSTED_PROXY_CIDRS"),
		CatalogTimeout:    8 * time.Second,
		TMDB:              clients.Catalog,
		Details:           clients.Details,
		Torrents:          clients.Torrents,
		IMDb:              clients.IMDb,
		TorrServer:        torrMgr,
		Engine:            sup,
		GStreamer:         newGStreamerInstaller(dataDir, torrMgr, sup),
		Version:           version,
		Updates:           releases,
		Events:            problems,
		SetupCode:         setupCode,
		LANAddress:        lanAddressFrom(os.Getenv("MT_LAN_ADDRESS")),
		AppsPort:          appsPort,
		AppsTLSPort:       appsTLSPort,
	})
	if err != nil {
		slog.Error("server configuration failed", "error", err)
		os.Exit(1)
	}

	// Stack middleware: Panic recovery (outermost) -> Security headers ->
	// Request logging -> Host allowlist (DNS rebinding) -> Router
	hosts := hostNames()
	handler := handlers.RecoveryMiddleware(handlers.SecurityHeadersMiddleware(secureCookies,
		handlers.LoggingMiddleware(handlers.HostAllowlistMiddleware(hosts, server))))

	addr := listenAddr()
	httpServer := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB header protection
	}
	// SSE streams never go idle, so Shutdown would wait out its grace period
	// on them; closing the server ends them as soon as shutdown begins.
	httpServer.RegisterOnShutdown(server.Close)

	// Server run context for graceful shutdown
	serverCtx, serverStopCtx := context.WithCancel(context.Background())

	// Listen for OS interrupt signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	// Started by the macOS app: stop when it is gone (force-quit, crash), so
	// no server or TorrServer is left holding the port.
	if pid, err := strconv.Atoi(os.Getenv("MT_PARENT_PID")); err == nil && pid > 1 {
		go func() {
			<-parentGone(pid, 2*time.Second)
			slog.Info("the app that started Moviestracker is gone")
			sigChan <- syscall.SIGTERM
		}()
	}
	// Started by the Windows tray app, which holds our input open: Windows
	// has no signals to stop a program with, and the input also closes when
	// the app is killed.
	if envBool("MT_STOP_WITH_STDIN", false) {
		go func() {
			<-inputClosed(os.Stdin)
			slog.Info("the app that started Moviestracker closed its input")
			sigChan <- syscall.SIGTERM
		}()
	}

	go func() {
		<-sigChan
		slog.Info("received shutdown signal, stopping server gracefully...")
		signal.Stop(sigChan)

		// Shutdown signal with grace period
		shutdownCtx, shutdownCancel := context.WithTimeout(serverCtx, 10*time.Second)
		defer shutdownCancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("http server shutdown failed", "error", err)
		}

		stopReleases()
		_ = appsPort.Close()
		_ = appsTLSPort.Close()
		// Close background stores cleanly
		server.Close()
		if sup != nil {
			_ = sup.Stop()
		}
		serverStopCtx()
	}()

	fmt.Printf("\n🎬 Moviestracker running at http://localhost%s\n", portSuffix(addr))
	if accounts.NeedsSetup() {
		fmt.Println("   First run: open it in a browser to create the administrator account.")
	}
	fmt.Println()
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server listen error", "error", err)
		os.Exit(1)
	}

	<-serverCtx.Done()
	slog.Info("server stopped cleanly")
}

// platform is how Moviestracker is released for this system: the Mac app,
// the Windows installer, or on Linux the Docker image.
func platform() update.Platform {
	switch runtime.GOOS {
	case "darwin":
		return update.Mac
	case "windows":
		return update.Windows
	}
	return update.Docker
}

// newEngine returns the supervisor of a managed TorrServer, or nil when no
// TorrServer program is installed. Whenever the engine (re)starts, torrMgr is
// pointed at it.
func newEngine(dataDir string, torrMgr *torrserver.Manager, startup config.EngineStartup) *engine.Supervisor {
	executable, _ := os.Executable()
	binary := engine.FindBinary(os.Getenv, executable, dataDir)
	if binary == "" {
		slog.Info("no TorrServer program found; managed mode is unavailable")
		return nil
	}
	slog.Info("TorrServer program", "path", binary)
	return engine.New(engine.Config{
		Binary:  binary,
		Dir:     filepath.Join(dataDir, "engine"),
		Options: handlers.EngineOptions(startup),
		// A managed engine is private to this machine: TorrServer's Bonjour
		// announcement of itself on the LAN is off unless the admin turns it on.
		FirstStart: func(ctx context.Context, url, user, password string) error {
			return torrserver.NewClient(url, nil, torrserver.WithBasicAuth(user, password)).
				UpdateSettings(ctx, map[string]any{"EnableBonjour": false})
		},
		OnReady: func(url, user, password string) {
			if err := torrMgr.SetEndpoint(url, user, password); err != nil {
				slog.Error("cannot use the engine address", "error", err)
			}
		},
	})
}

// gstRoot is where the macOS app's GStreamer downloads go.
func gstRoot(dataDir string) string { return filepath.Join(dataDir, "gstreamer") }

// newGStreamerInstaller lets admins download GStreamer from the web pages
// when Moviestracker runs TorrServer in the macOS app, whose bundle holds
// install-gstreamer.sh (MT_GSTREAMER_SCRIPT overrides it, for development).
func newGStreamerInstaller(dataDir string, torrMgr *torrserver.Manager, sup *engine.Supervisor) *gstinstall.Installer {
	if runtime.GOOS != "darwin" || sup == nil {
		return nil
	}
	script := strings.TrimSpace(os.Getenv("MT_GSTREAMER_SCRIPT"))
	if script == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil
		}
		script = filepath.Join(filepath.Dir(exe), "..", "Resources", "install-gstreamer.sh")
	}
	// #nosec G703 -- the app bundle's own script, or the operator's MT_GSTREAMER_SCRIPT
	if info, err := os.Stat(script); err != nil || info.Mode().Perm()&0o111 == 0 {
		return nil
	}
	root := gstRoot(dataDir)
	slog.Info("GStreamer can be downloaded from Settings → Sources", "script", script)
	use := func(ctx context.Context, dir string) error {
		useAppGStreamer(ctx, torrMgr.Client(), sup, dir, root)
		return nil
	}
	// Ready only once the restarted TorrServer runs with GStreamer.
	running := func(ctx context.Context) bool {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		echo, err := torrMgr.Client().Echo(ctx)
		return err == nil && echo.GSTAvailable
	}
	return gstinstall.New(script, root, use, gstinstall.WithCheck(running, time.Second, 90*time.Second))
}

// useAppGStreamer points the managed TorrServer at dir, a GStreamer the
// macOS app downloaded under root, and restarts it once so it loads it.
func useAppGStreamer(ctx context.Context, client *torrserver.Client, sup *engine.Supervisor, dir, root string) {
	changed, err := client.UseAppGStreamer(ctx, dir, root)
	if err != nil {
		slog.Warn("cannot point TorrServer at the downloaded GStreamer", "error", err)
		return
	}
	if !changed {
		return
	}
	slog.Info("TorrServer uses the downloaded GStreamer; restarting it", "dir", dir)
	if err := sup.Restart(ctx); err != nil {
		slog.Error("TorrServer did not restart", "error", err)
	}
}

// settleEngineMode saves the engine mode on the first start, so installing a
// TorrServer program later never switches an install away from the
// TorrServer (and torrent list) it already uses.
func settleEngineMode(store *config.Store, sup *engine.Supervisor) error {
	if store.State().TorrServer.Mode != "" {
		return nil
	}
	// An install with accounts predates engine modes and used the TorrServer
	// at its URL; only a fresh install starts with the managed engine.
	mode := config.EngineExternal
	if sup != nil && len(store.State().Users) == 0 {
		mode = config.EngineManaged
	}
	return store.Update(func(st *config.State) error {
		st.TorrServer.Mode = mode
		return nil
	})
}

// managedEngine reports whether Moviestracker should run TorrServer: chosen
// in Sources with a program available, and not overridden by TORRSERVER_URL.
func managedEngine(env config.Env, ts config.TorrServer, sup *engine.Supervisor) bool {
	return sup != nil && env.TorrServerURL == "" && ts.Mode == config.EngineManaged
}

// setupCodeAlphabet leaves out 0/O and 1/I/L, which are easy to mistype.
const setupCodeAlphabet = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

// newSetupCode returns a random XXXX-XXXX code from entropy.
func newSetupCode(entropy io.Reader) (string, error) {
	size := big.NewInt(int64(len(setupCodeAlphabet)))
	code := make([]byte, 0, 9)
	for i := range 8 {
		if i == 4 {
			code = append(code, '-')
		}
		n, err := rand.Int(entropy, size)
		if err != nil {
			return "", fmt.Errorf("setup code: %w", err)
		}
		code = append(code, setupCodeAlphabet[n.Int64()])
	}
	return string(code), nil
}

// defaultListen serves the whole LAN (TVs, phones) on port 8095; every page
// requires signing in.
const defaultListen = ":8095"

// listenAddr is MT_LISTEN (host:port), else :PORT for development tools, else :8095.
func listenAddr() string {
	if addr := strings.TrimSpace(os.Getenv("MT_LISTEN")); addr != "" {
		return addr
	}
	if port := strings.TrimSpace(os.Getenv("PORT")); port != "" {
		return ":" + port
	}
	return defaultListen
}

// defaultAppsListen is where other apps find TorrServer: 8090 is the port
// TorrServe and Lampa suggest.
const defaultAppsListen = ":8090"

// appsListenAddr is MT_TORRSERVER_LISTEN (host:port), else :8090.
func appsListenAddr() string {
	if addr := strings.TrimSpace(os.Getenv("MT_TORRSERVER_LISTEN")); addr != "" {
		return addr
	}
	return defaultAppsListen
}

// defaultAppsTLSListen is where other apps find TorrServer over HTTPS: 8091,
// TorrServer's own HTTPS port.
const defaultAppsTLSListen = ":8091"

// appsTLSListenAddr is MT_TORRSERVER_TLS_LISTEN (host:port), else :8091.
func appsTLSListenAddr() string {
	if addr := strings.TrimSpace(os.Getenv("MT_TORRSERVER_TLS_LISTEN")); addr != "" {
		return addr
	}
	return defaultAppsTLSListen
}

// checkWritable makes sure this user can write to dir, and says how to fix
// it otherwise (a folder mounted into the container belongs to someone else).
func checkWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".write-check-*")
	if err != nil {
		uid, gid := os.Getuid(), os.Getgid()
		return fmt.Errorf("%s is not writable by uid %d: give it to that user (chown -R %d:%d %s) or run Moviestracker as its owner: %w",
			dir, uid, uid, gid, dir, err)
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}

// lanAddressFrom reads MT_LAN_ADDRESS, the address TVs and phones use to
// reach this machine in links made while it is opened as localhost: empty
// finds it (nil), "off" keeps localhost (the container image: its own
// address is not the host's), anything else is that address.
func lanAddressFrom(value string) func() string {
	value = strings.TrimSpace(value)
	switch value {
	case "":
		return nil
	case "off":
		return func() string { return "" }
	default:
		return func() string { return value }
	}
}

// checkHealth asks the server listening on listen (MT_LISTEN) for /healthz,
// from this machine: the container health check.
func checkHealth(listen string) error {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return err
	}
	if host == "" || net.ParseIP(host).IsUnspecified() {
		host = "127.0.0.1"
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(string(body), "ok") {
		return fmt.Errorf("/healthz answered %d %q", resp.StatusCode, body)
	}
	return nil
}

// portSuffix returns the ":port" part of a listen address.
func portSuffix(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[i:]
	}
	return ""
}

// resolveDataDir is MT_DATA_DIR, else "moviestracker" in the user's config
// directory (~/Library/Application Support on macOS, ~/.config on Linux).
func resolveDataDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("MT_DATA_DIR")); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("set MT_DATA_DIR: %w", err)
	}
	return filepath.Join(base, "moviestracker"), nil
}

// hostNames are the hostnames the server answers to besides IP addresses and
// localhost: MT_HOSTNAMES plus this machine's name and its .local mDNS name.
func hostNames() []string {
	names := envList("MT_HOSTNAMES")
	if host, err := os.Hostname(); err == nil && host != "" {
		host = strings.TrimSuffix(host, ".local")
		names = append(names, host, host+".local")
	}
	return names
}

func envBool(name string, defaultValue bool) bool {
	value, ok := os.LookupEnv(name)
	if !ok || value == "" {
		return defaultValue
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		slog.Warn("invalid boolean environment value; using default", "name", name, "default", defaultValue)
		return defaultValue
	}
	return parsed
}

func envList(name string) []string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			values = append(values, part)
		}
	}
	return values
}

func loadDotEnv() {
	const envFile = ".env"
	file, err := os.Open(envFile)
	if err != nil {
		return // File does not exist or unreadable, ignore
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, `"'`)
		if _, exists := os.LookupEnv(key); !exists {
			_ = os.Setenv(key, val)
		}
	}
}

// inputClosed is closed when in reaches its end (or fails): the program
// that holds its other end has closed it or is gone.
func inputClosed(in io.Reader) <-chan struct{} {
	closed := make(chan struct{})
	go func() {
		defer close(closed)
		_, _ = io.Copy(io.Discard, in)
	}()
	return closed
}

// parentGone is closed once this process's parent is no longer pid: the
// parent exited and the process was handed to launchd or init.
func parentGone(pid int, every time.Duration) <-chan struct{} {
	gone := make(chan struct{})
	go func() {
		defer close(gone)
		for os.Getppid() == pid {
			time.Sleep(every)
		}
	}()
	return gone
}
