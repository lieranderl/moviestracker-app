// Command web is Moviestracker's cloud web app, run on Cloud Run
// (Dockerfile.web). The local app is cmd/server.
package main

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/config"
	"github.com/lieranderl/moviestracker-app/internal/sources"
	"github.com/lieranderl/moviestracker-app/internal/store"
	"github.com/lieranderl/moviestracker-app/internal/web"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg := configFromEnv()
	users, err := openStore(context.Background())
	if err != nil {
		slog.Error("cannot open the user store", "error", err)
		os.Exit(1)
	}
	defer func() { _ = users.Close() }()
	cfg.Store = users

	httpServer := &http.Server{
		Addr:              listenAddr(),
		Handler:           web.New(cfg),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB header protection
	}

	// Cloud Run sends SIGTERM and allows 10 seconds before it stops the
	// instance.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		slog.Info("received shutdown signal, stopping server gracefully...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("http server shutdown failed", "error", err)
		}
	}()

	slog.Info("moviestracker web listening", "addr", httpServer.Addr)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server listen error", "error", err)
		os.Exit(1)
	}
	<-done
	slog.Info("server stopped cleanly")
}

// listenAddr is the port Cloud Run gives in PORT, else 8080.
func listenAddr() string {
	if port := os.Getenv("PORT"); port != "" {
		return ":" + port
	}
	return ":8080"
}

// configFromEnv is the web app's configuration from its environment (Cloud
// Run's service settings, or .env.web locally: see .env.web.example).
func configFromEnv() web.Config {
	cfg := web.Config{
		Sources: sources.Connector{}.Connect(config.Sources{
			TMDBKey: os.Getenv("MT_WEB_TMDB_KEY"),
			IMDbURL: cmp.Or(os.Getenv("MT_WEB_IMDB_URL"), config.DefaultIMDbURL),
		}),
		BaseURL:    strings.TrimRight(os.Getenv("MT_WEB_BASE_URL"), "/"),
		SessionKey: []byte(os.Getenv("MT_WEB_SESSION_KEY")),
		Google: web.Google{
			ClientID:     os.Getenv("MT_WEB_GOOGLE_CLIENT_ID"),
			ClientSecret: os.Getenv("MT_WEB_GOOGLE_CLIENT_SECRET"),
		},
	}
	if cfg.Google.ClientID == "" || cfg.Google.ClientSecret == "" || len(cfg.SessionKey) < 32 || cfg.BaseURL == "" {
		slog.Warn("sign-in is off: set MT_WEB_BASE_URL, MT_WEB_SESSION_KEY (32+ characters), MT_WEB_GOOGLE_CLIENT_ID and MT_WEB_GOOGLE_CLIENT_SECRET")
	}
	if cfg.Sources.Catalog == nil {
		slog.Warn("the catalog is empty: set MT_WEB_TMDB_KEY")
	}
	return cfg
}

// userStore is the store the app keeps users' data in.
type userStore interface {
	store.Store
	Close() error
}

// memoryStore is the in-memory store, with nothing to close.
type memoryStore struct{ *store.Memory }

func (memoryStore) Close() error { return nil }

// openStore opens Firestore when MT_WEB_FIRESTORE_PROJECT names its project
// (the database is MT_WEB_FIRESTORE_DATABASE, else "moviestracker"; with
// FIRESTORE_EMULATOR_HOST set, the emulator there). Without it, users' data
// is kept in memory until the app stops.
func openStore(ctx context.Context) (userStore, error) {
	project := os.Getenv("MT_WEB_FIRESTORE_PROJECT")
	if project == "" {
		slog.Warn("users' preferences and favourites are kept in memory: set MT_WEB_FIRESTORE_PROJECT to keep them in Firestore")
		return memoryStore{store.NewMemory()}, nil
	}
	database := os.Getenv("MT_WEB_FIRESTORE_DATABASE")
	if database == "" {
		database = "moviestracker"
	}
	return store.NewFirestore(ctx, project, database)
}
