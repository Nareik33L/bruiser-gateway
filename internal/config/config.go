package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr      string
	DatabaseURL   string
	DevHMACSecret string
	MerchantID    string
	MerchantName  string
	LeaseTTL      time.Duration
	MaxLifetime   time.Duration
	MaxActive     int
	SessionTTL    time.Duration
	LogLevel      string
	ReadyTimeout  time.Duration
	SweepInterval time.Duration
	EdgeSecret    string
	OriginSecret  string
	ProfilePath   string
	ProxyAddr     string
	OriginURL     string
	MaxInFlight   int
	RatePerSec    float64
}

func Load() Config {
	return Config{
		HTTPAddr:      env("BRUISER_HTTP_ADDR", ":8080"),
		DatabaseURL:   env("BRUISER_DATABASE_URL", "postgres://bruiser:bruiser@127.0.0.1:5432/bruiser?sslmode=disable"),
		DevHMACSecret: env("BRUISER_DEV_HMAC_SECRET", "dev-secret-change-me"),
		MerchantID:    env("BRUISER_MERCHANT_ID", "arsenal"),
		MerchantName:  env("BRUISER_MERCHANT_NAME", "Arsenal FC"),
		LeaseTTL:      envDuration("BRUISER_LEASE_TTL", 60*time.Second),
		MaxLifetime:   envDuration("BRUISER_MAX_LIFETIME", 15*time.Minute),
		MaxActive:     envInt("BRUISER_MAX_ACTIVE", 1),
		SessionTTL:    envDuration("BRUISER_SESSION_TTL", time.Hour),
		LogLevel:      env("BRUISER_LOG_LEVEL", "info"),
		ReadyTimeout:  envDuration("BRUISER_READY_TIMEOUT", 2*time.Second),
		SweepInterval: envDuration("BRUISER_SWEEP_INTERVAL", 2*time.Second),
		EdgeSecret:    env("BRUISER_EDGE_SECRET", "edge-secret-dev"),
		OriginSecret:  env("BRUISER_ORIGIN_SECRET", "origin-lock-dev"),
		ProfilePath:   env("BRUISER_PROFILE", "configs/arsenal.yaml"),
		ProxyAddr:     env("BRUISER_PROXY_ADDR", ""),
		OriginURL:     env("BRUISER_ORIGIN_URL", ""),
		MaxInFlight:   envInt("BRUISER_MAX_IN_FLIGHT", 2),
		RatePerSec:    envFloat("BRUISER_RATE_PER_SEC", 5),
	}
}

func (c Config) Validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("BRUISER_DATABASE_URL is required")
	}
	if c.DevHMACSecret == "" {
		return fmt.Errorf("BRUISER_DEV_HMAC_SECRET is required")
	}
	if c.MaxActive < 1 {
		return fmt.Errorf("BRUISER_MAX_ACTIVE must be >= 1")
	}
	return nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			return d
		}
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil {
			return n
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		n, err := strconv.ParseFloat(v, 64)
		if err == nil {
			return n
		}
	}
	return def
}
