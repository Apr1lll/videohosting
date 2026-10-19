// Package config загружает настройки сервиса из переменных окружения.
package config

import (
	"os"
	"time"
)

type Config struct {
	HTTPAddr    string
	DatabaseURL string
	JWTSecret   string
	TokenTTL    time.Duration
}

func Load() Config {
	ttl, err := time.ParseDuration(getEnv("TOKEN_TTL", "24h"))
	if err != nil {
		ttl = 24 * time.Hour
	}

	return Config{
		HTTPAddr:    getEnv("HTTP_ADDR", ":8081"),
		DatabaseURL: getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/auth_db?sslmode=disable"),
		JWTSecret:   getEnv("JWT_SECRET", "dev-secret-change-me"),
		TokenTTL:    ttl,
	}
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
