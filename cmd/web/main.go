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
	"syscall"
	"time"

	"github.com/lieranderl/moviestracker-app/internal/web"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	httpServer := &http.Server{
		Addr:              listenAddr(),
		Handler:           web.New(),
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
