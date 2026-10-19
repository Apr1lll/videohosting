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

	"github.com/redis/go-redis/v9"

	"videohosting/video-service/internal/cache"
	"videohosting/video-service/internal/client"
	"videohosting/video-service/internal/config"
	"videohosting/video-service/internal/database"
	"videohosting/video-service/internal/handler"
	"videohosting/video-service/internal/repository"
	"videohosting/video-service/internal/service"
	"videohosting/video-service/internal/storage"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", "video-service"))
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("cannot connect to database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	if err := rdb.Ping(ctx).Err(); err != nil {
		slog.Error("cannot connect to redis", "err", err)
		os.Exit(1)
	}
	defer rdb.Close()

	fileStorage, err := storage.NewLocalStorage(cfg.StoragePath)
	if err != nil {
		slog.Error("cannot init storage", "err", err)
		os.Exit(1)
	}

	svc := service.NewVideoService(
		repository.NewVideoRepository(pool),
		cache.New(rdb, cfg.CacheTTL),
		fileStorage,
		client.NewInteractionClient(cfg.InteractionServiceURL),
	)
	h := handler.New(svc, cfg.MaxUploadBytes)

	flusherDone := make(chan struct{})
	go func() {
		svc.RunViewsFlusher(ctx, cfg.ViewsFlushInterval)
		close(flusherDone)
	}()

	// Read/WriteTimeout не задаём: загрузка и просмотр больших видео могут идти долго.
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           h.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
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
	<-flusherDone
}
