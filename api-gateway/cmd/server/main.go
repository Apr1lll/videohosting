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

	"videohosting/api-gateway/internal/auth"
	"videohosting/api-gateway/internal/config"
	"videohosting/api-gateway/internal/gateway"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "api-gateway"))
	cfg := config.Load()

	gw, err := gateway.New(
		auth.NewVerifier(cfg.JWTSecret),
		cfg.AuthServiceURL,
		cfg.VideoServiceURL,
		cfg.InteractionServiceURL,
		cfg.WebDir,
	)
	if err != nil {
		slog.Error("invalid backend url", "err", err)
		os.Exit(1)
	}

	limiter := gateway.NewRateLimiter(cfg.RateLimitPerMinute)

	// Порядок middleware: RequestID -> Logging -> CORS -> RateLimit -> Gateway.
	var handler http.Handler = gw
	handler = limiter.Middleware(handler)
	handler = gateway.CORS(handler)
	handler = gateway.Logging(handler)
	handler = gateway.RequestID(handler)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("gateway started", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
	}
}
