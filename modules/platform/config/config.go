package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"time"
)

type Config struct {
	Environment     string
	Address         string
	JWTKey          []byte
	JWTIssuer       string
	JWTAudience     string
	AccessTTL       time.Duration
	RefreshTTL      time.Duration
	DatabasePath    string
	ShutdownTimeout time.Duration
}

func Load() (Config, error) {
	c := Config{Environment: value("APP_ENV", "development"), Address: value("HTTP_ADDR", ":8080"),
		JWTIssuer: value("JWT_ISSUER", "ecommerce-api"), JWTAudience: value("JWT_AUDIENCE", "ecommerce-clients"), DatabasePath: value("DB_PATH", "./data/ecommerce.db"), ShutdownTimeout: 10 * time.Second}
	if c.Environment != "development" && c.Environment != "demo" && c.Environment != "production" {
		return Config{}, fmt.Errorf("APP_ENV must be development, demo, or production")
	}
	key, err := base64.StdEncoding.DecodeString(os.Getenv("JWT_SECRET"))
	if err != nil || len(key) < 32 || len(key) > 64 {
		return Config{}, fmt.Errorf("JWT_SECRET must be base64 encoding of 32–64 random bytes; run make init-env")
	}
	c.JWTKey = key
	c.AccessTTL, err = time.ParseDuration(value("ACCESS_TOKEN_TTL", "15m"))
	if err != nil || c.AccessTTL < time.Minute || c.AccessTTL > time.Hour {
		return Config{}, fmt.Errorf("ACCESS_TOKEN_TTL must be between 1m and 1h")
	}
	c.RefreshTTL, err = time.ParseDuration(value("REFRESH_TOKEN_TTL", "720h"))
	if err != nil || c.RefreshTTL < time.Hour || c.RefreshTTL > 90*24*time.Hour || c.RefreshTTL <= c.AccessTTL {
		return Config{}, fmt.Errorf("REFRESH_TOKEN_TTL must be greater than access TTL and between 1h and 2160h")
	}
	return c, nil
}

func value(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
