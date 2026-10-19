// Package config загружает настройки сервиса из переменных окружения.
package config

import (
	"os"
	"time"
)

type Config struct {
	HTTPAddr        string
	DatabaseURL     string
	RedisAddr       string
	CacheTTL        time.Duration
	VideoServiceURL string
	AuthServiceURL  string
}

func Load() Config {
	ttl, err := time.ParseDuration(getEnv("CACHE_TTL", "10m"))
	if err != nil {
		ttl = 10 * time.Minute
	}
	return Config{
		HTTPAddr:        getEnv("HTTP_ADDR", ":8083"),
		DatabaseURL:     getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/interaction_db?sslmode=disable"),
		RedisAddr:       getEnv("REDIS_ADDR", "localhost:6379"),
		CacheTTL:        ttl,
		VideoServiceURL: getEnv("VIDEO_SERVICE_URL", "http://localhost:8082"),
		AuthServiceURL:  getEnv("AUTH_SERVICE_URL", "http://localhost:8081"),
	}
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
