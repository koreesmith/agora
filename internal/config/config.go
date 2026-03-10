package config

import (
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	HTTPAddr       string
	InstanceDomain string
	InstanceName   string
	JWTSecret      string
	DatabaseURL    string
	RedisURL       string
	UploadDir      string
	Environment    string
	AllowedOrigins []string

	SMTPHost     string
	SMTPPort     string
	SMTPUser     string
	SMTPPassword string
	SMTPFrom     string
	SMTPEnabled  bool
}

func Load() *Config {
	// Load .env if present (dev convenience — ignored if missing)
	_ = godotenv.Load()

	domain := getEnv("INSTANCE_DOMAIN", "http://localhost")

	origins := []string{domain}
	if env := getEnv("ENVIRONMENT", "production"); env == "development" {
		origins = append(origins,
			"http://localhost",
			"http://localhost:80",
			"http://localhost:3000",
			"http://localhost:5173",
		)
	}
	if extra := getEnv("ALLOWED_ORIGINS", ""); extra != "" {
		for _, o := range strings.Split(extra, ",") {
			o = strings.TrimSpace(o)
			if o != "" {
				origins = append(origins, o)
			}
		}
	}

	return &Config{
		HTTPAddr:       getEnv("HTTP_ADDR", ":8080"),
		InstanceDomain: domain,
		InstanceName:   getEnv("INSTANCE_NAME", "Agora"),
		JWTSecret:      getEnv("JWT_SECRET", "dev-secret-change-in-production"),
		DatabaseURL:    getEnv("DATABASE_URL", "postgres://agora:agora@localhost:5432/agora?sslmode=disable"),
		RedisURL:       getEnv("REDIS_URL", "redis://localhost:6379"),
		UploadDir:      getEnv("UPLOAD_DIR", "./data/uploads"),
		Environment:    getEnv("ENVIRONMENT", "development"),
		AllowedOrigins: origins,

		SMTPHost:     getEnv("SMTP_HOST", ""),
		SMTPPort:     getEnv("SMTP_PORT", "587"),
		SMTPUser:     getEnv("SMTP_USER", ""),
		SMTPPassword: getEnv("SMTP_PASSWORD", ""),
		SMTPFrom:     getEnv("SMTP_FROM", "noreply@localhost"),
		SMTPEnabled:  getEnv("SMTP_ENABLED", "false") == "true",
	}
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}
