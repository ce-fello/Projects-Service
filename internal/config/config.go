package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	HTTPPort       string
	DatabaseURL    string
	JWTSecret      string
	EnableDemoSeed bool
}

func Load() (Config, error) {
	enableDemoSeed, err := getEnvBool("ENABLE_DEMO_SEED", false)
	if err != nil {
		return Config{}, fmt.Errorf("ENABLE_DEMO_SEED is invalid: %w", err)
	}

	cfg := Config{
		HTTPPort:       getEnv("HTTP_PORT", "8000"),
		DatabaseURL:    getEnv("DATABASE_URL", "postgres://postgres:postgres@postgres:5432/projects_service?sslmode=disable"),
		JWTSecret:      os.Getenv("JWT_SECRET"),
		EnableDemoSeed: enableDemoSeed,
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}

	if cfg.JWTSecret == "" {
		return Config{}, fmt.Errorf("JWT_SECRET is required")
	}

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}

	return fallback
}

func getEnvBool(key string, fallback bool) (bool, error) {
	value, ok := os.LookupEnv(key)
	if !ok {
		return fallback, nil
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, err
	}

	return parsed, nil
}
