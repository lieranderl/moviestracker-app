// Command web is Moviestracker's cloud web app, run on Cloud Run
// (Dockerfile.web). The local app is cmd/server.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/web"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	httpServer := &http.Server{
		Addr:              listenAddr(),
		Handler:           web.New(configFromEnv()),
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
	return cfg
}
