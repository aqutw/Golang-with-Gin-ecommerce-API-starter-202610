package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	Environment     string
	Address         string
	APIToken        string
	Subject         string
	DatabasePath    string
	ShutdownTimeout time.Duration
}

func Load() (Config, error) {
	c := Config{Environment: value("APP_ENV", "development"), Address: value("HTTP_ADDR", ":8080"),
		APIToken: os.Getenv("API_TOKEN"), Subject: value("API_SUBJECT", "demo-customer"), DatabasePath: value("DB_PATH", "./data/ecommerce.db"), ShutdownTimeout: 10 * time.Second}
	if c.Environment != "development" && c.Environment != "demo" && c.Environment != "production" {
		return Config{}, fmt.Errorf("APP_ENV must be development, demo, or production")
	}
	if len(c.APIToken) < 16 {
		return Config{}, fmt.Errorf("API_TOKEN must contain at least 16 characters")
	}
	return c, nil
}

func value(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
