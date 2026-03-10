package config

import (
	"os"
	"strings"
)

type Config struct {
	DatabaseURL    string
	RedisURL       string
	JWTSecret      string
	Port           string
	MediaDir       string
	InstanceURL    string
	Environment    string
	AllowedOrigins []string
}

func Load() *Config {
	port := getEnv("PORT", "8080")
	instanceURL := getEnv("INSTANCE_URL", "http://localhost")

	origins := []string{instanceURL}
	if extra := getEnv("ALLOWED_ORIGINS", ""); extra != "" {
		origins = append(origins, strings.Split(extra, ",")...)
	}
	// Always allow localhost in development
	if getEnv("ENVIRONMENT", "development") == "development" {
		origins = append(origins, "http://localhost:3000", "http://localhost:5173")
	}

	return &Config{
		DatabaseURL:    getEnv("DATABASE_URL", "postgres://agora:agorapass@localhost:5432/agora?sslmode=disable"),
		RedisURL:       getEnv("REDIS_URL", "redis://localhost:6379"),
		JWTSecret:      getEnv("JWT_SECRET", "dev-secret-change-in-production"),
		Port:           port,
		MediaDir:       getEnv("MEDIA_DIR", "./media"),
		InstanceURL:    instanceURL,
		Environment:    getEnv("ENVIRONMENT", "development"),
		AllowedOrigins: origins,
	}
}

func getEnv(key, defaultValue string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return defaultValue
}
