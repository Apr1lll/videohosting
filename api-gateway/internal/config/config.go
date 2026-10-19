// Package config загружает настройки gateway из переменных окружения.
package config

import (
	"os"
	"strconv"
)

type Config struct {
	HTTPAddr              string
	JWTSecret             string
	AuthServiceURL        string
	VideoServiceURL       string
	InteractionServiceURL string
	RateLimitPerMinute    int
	WebDir                string
}

func Load() Config {
	rate, err := strconv.Atoi(getEnv("RATE_LIMIT_PER_MINUTE", "300"))
	if err != nil || rate <= 0 {
		rate = 300
	}
	return Config{
		HTTPAddr:              getEnv("HTTP_ADDR", ":8080"),
		JWTSecret:             getEnv("JWT_SECRET", "dev-secret-change-me"),
		AuthServiceURL:        getEnv("AUTH_SERVICE_URL", "http://localhost:8081"),
		VideoServiceURL:       getEnv("VIDEO_SERVICE_URL", "http://localhost:8082"),
		InteractionServiceURL: getEnv("INTERACTION_SERVICE_URL", "http://localhost:8083"),
		RateLimitPerMinute:    rate,
		WebDir:                getEnv("WEB_DIR", "./web"),
	}
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
