// Package config загружает настройки сервиса из переменных окружения.
package config

import (
	"os"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr              string
	DatabaseURL           string
	JWTSecret             string
	TokenTTL              time.Duration
	AvatarPath            string
	AdminUsernames        []string // эти пользователи автоматически получают роль admin
	VideoServiceURL       string
	InteractionServiceURL string
}

func Load() Config {
	ttl, err := time.ParseDuration(getEnv("TOKEN_TTL", "24h"))
	if err != nil {
		ttl = 24 * time.Hour
	}

	var admins []string
	for _, name := range strings.Split(getEnv("ADMIN_USERNAMES", "apr1l"), ",") {
		if name = strings.TrimSpace(name); name != "" {
			admins = append(admins, name)
		}
	}

	return Config{
		HTTPAddr:              getEnv("HTTP_ADDR", ":8081"),
		DatabaseURL:           getEnv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/auth_db?sslmode=disable"),
		JWTSecret:             getEnv("JWT_SECRET", "dev-secret-change-me"),
		TokenTTL:              ttl,
		AvatarPath:            getEnv("AVATAR_PATH", "./data/avatars"),
		AdminUsernames:        admins,
		VideoServiceURL:       getEnv("VIDEO_SERVICE_URL", "http://localhost:8082"),
		InteractionServiceURL: getEnv("INTERACTION_SERVICE_URL", "http://localhost:8083"),
	}
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
