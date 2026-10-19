// Package config загружает настройки сервиса из переменных окружения.
package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr              string
	DatabaseURL           string
	RedisAddr             string
	StoragePath           string
	MaxUploadBytes        int64
	CacheTTL              time.Duration
	ViewsFlushInterval    time.Duration
	InteractionServiceURL string
}

func Load() Config {
	maxMB, err := strconv.ParseInt(getEnv("MAX_UPLOAD_MB", "500"), 10, 64)
	if err != nil || maxMB <= 0 {
		maxMB = 500
	}

	return Config{
		HTTPAddr:              getEnv("HTTP_ADDR", ":8082"),
		DatabaseURL:           getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/video_db?sslmode=disable"),
		RedisAddr:             getEnv("REDIS_ADDR", "localhost:6379"),
		StoragePath:           getEnv("STORAGE_PATH", "./data/videos"),
		MaxUploadBytes:        maxMB << 20,
		CacheTTL:              getDuration("CACHE_TTL", 5*time.Minute),
		ViewsFlushInterval:    getDuration("VIEWS_FLUSH_INTERVAL", 30*time.Second),
		InteractionServiceURL: getEnv("INTERACTION_SERVICE_URL", "http://localhost:8083"),
	}
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getDuration(key string, def time.Duration) time.Duration {
	d, err := time.ParseDuration(getEnv(key, ""))
	if err != nil {
		return def
	}
	return d
}
