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

	"videohosting/auth-service/internal/client"
	"videohosting/auth-service/internal/config"
	"videohosting/auth-service/internal/database"
	"videohosting/auth-service/internal/handler"
	"videohosting/auth-service/internal/repository"
	"videohosting/auth-service/internal/service"
	"videohosting/auth-service/internal/storage"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "auth-service"))
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("cannot connect to database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	avatars, err := storage.NewLocalStorage(cfg.AvatarPath)
	if err != nil {
		slog.Error("cannot init avatar storage", "err", err)
		os.Exit(1)
	}

	svc := service.NewAuthService(
		repository.NewUserRepository(pool),
		avatars,
		client.NewUserDataCleaner(cfg.VideoServiceURL, cfg.InteractionServiceURL),
		cfg.AdminUsernames,
		cfg.JWTSecret,
		cfg.TokenTTL,
	)
	h := handler.New(svc)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           h.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      90 * time.Second, // удаление пользователя с большим числом видео
	}

	go func() {
		slog.Info("server started", "addr", cfg.HTTPAddr)
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
