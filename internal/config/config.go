package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr          string
	DatabaseURL       string
	DevHMACSecret     string
	MerchantID        string
	MerchantName      string
	LeaseTTL          time.Duration
	HeartbeatInterval time.Duration
	MaxLifetime       time.Duration
	MaxActive         int
	SessionTTL        time.Duration
	LogLevel          string
	ReadyTimeout      time.Duration
	SweepInterval     time.Duration
	EdgeSecret        string
	OriginSecret      string
	ProfilePath       string
	ProxyAddr         string
	OriginURL         string
	MaxInFlight       int
	RatePerSec        float64
	AuditRetention    time.Duration
	CheckEdgeURL      string
	CheckOriginURL    string
	AdminSecret       string
	Telemetry         bool
	IntrospectURL     string
	Burst             int
	Mode              string
	Enforcement       bool
	QueueEnabled      bool
	EnforcePercent    int
	Environment       string
	DevAssertions     bool
	JWKSURL           string
	Issuer            string
	Audience          string
	RateSessions      float64
	RateAcquire       float64
	RateRenew         float64
	RateRelease       float64
	RateAuthorize     float64
	RateMerchant      float64
	RateCustomer      float64
	RatePrincipal     float64
	RateIP            float64
}

func Load() Config {
	c := Config{
		HTTPAddr:          env("BRUISER_HTTP_ADDR", ":8080"),
		DatabaseURL:       env("BRUISER_DATABASE_URL", "postgres://bruiser:bruiser@127.0.0.1:5432/bruiser?sslmode=disable"),
		DevHMACSecret:     env("BRUISER_DEV_HMAC_SECRET", "dev-secret-change-me"),
		MerchantID:        env("BRUISER_MERCHANT_ID", "arsenal"),
		MerchantName:      env("BRUISER_MERCHANT_NAME", "Arsenal FC"),
		LeaseTTL:          envDuration("BRUISER_LEASE_TTL", 60*time.Second),
		HeartbeatInterval: envDuration("BRUISER_HEARTBEAT_INTERVAL", 20*time.Second),
		MaxLifetime:       envDuration("BRUISER_MAX_LIFETIME", 15*time.Minute),
		MaxActive:         envInt("BRUISER_MAX_ACTIVE", 1),
		SessionTTL:        envDuration("BRUISER_SESSION_TTL", time.Hour),
		LogLevel:          env("BRUISER_LOG_LEVEL", "info"),
		ReadyTimeout:      envDuration("BRUISER_READY_TIMEOUT", 2*time.Second),
		SweepInterval:     envDuration("BRUISER_SWEEP_INTERVAL", 2*time.Second),
		EdgeSecret:        env("BRUISER_EDGE_SECRET", "edge-secret-dev"),
		OriginSecret:      env("BRUISER_ORIGIN_SECRET", "origin-lock-dev"),
		ProfilePath:       env("BRUISER_PROFILE", "configs/arsenal.yaml"),
		ProxyAddr:         env("BRUISER_PROXY_ADDR", ""),
		OriginURL:         env("BRUISER_ORIGIN_URL", ""),
		MaxInFlight:       envInt("BRUISER_MAX_IN_FLIGHT", 2),
		RatePerSec:        envFloat("BRUISER_RATE_PER_SEC", 5),
		AuditRetention:    envDuration("BRUISER_AUDIT_RETENTION", 13*30*24*time.Hour),
		CheckEdgeURL:      env("BRUISER_CHECK_EDGE_URL", "http://127.0.0.1:8091"),
		CheckOriginURL:    env("BRUISER_CHECK_ORIGIN_URL", "http://127.0.0.1:8090"),
		AdminSecret:       lookupEnv("BRUISER_ADMIN_SECRET"),
		Telemetry:         env("BRUISER_TELEMETRY", "") == "1" || strings.EqualFold(env("BRUISER_TELEMETRY", ""), "true"),
		IntrospectURL:     env("BRUISER_INTROSPECT_URL", ""),
		Burst:             envInt("BRUISER_BURST", 10),
		Mode:              env("BRUISER_MODE", "enforce"),
		Enforcement:       envBool("BRUISER_ENFORCEMENT", true),
		QueueEnabled:      envBool("BRUISER_QUEUE", true),
		EnforcePercent:    envInt("BRUISER_ENFORCE_PERCENT", -1),
		Environment:       env("BRUISER_ENV", env("BRUISER_ENVIRONMENT", "")),
		JWKSURL:           env("BRUISER_JWKS_URL", ""),
		Issuer:            env("BRUISER_ISSUER", ""),
		Audience:          env("BRUISER_AUDIENCE", ""),
		RateSessions:      envFloat("BRUISER_RATE_SESSIONS", 20),
		RateAcquire:       envFloat("BRUISER_RATE_ACQUIRE", 20),
		RateRenew:         envFloat("BRUISER_RATE_RENEW", 40),
		RateRelease:       envFloat("BRUISER_RATE_RELEASE", 20),
		RateAuthorize:     envFloat("BRUISER_RATE_AUTHORIZE", 40),
		RateMerchant:      envFloat("BRUISER_RATE_MERCHANT", 200),
		RateCustomer:      envFloat("BRUISER_RATE_CUSTOMER", 40),
		RatePrincipal:     envFloat("BRUISER_RATE_PRINCIPAL", 20),
		RateIP:            envFloat("BRUISER_RATE_IP", 0),
	}
	c.DevAssertions = envBool("BRUISER_DEV_ASSERTIONS", !c.Production())
	return c
}

// Production is true when the operator set BRUISER_ENV to a live environment.
func (c Config) Production() bool {
	switch strings.ToLower(strings.TrimSpace(c.Environment)) {
	case "production", "prod", "live":
		return true
	default:
		return false
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
	switch strings.ToLower(strings.TrimSpace(c.Mode)) {
	case "", "enforce", "dry-run":
	default:
		return fmt.Errorf("BRUISER_MODE must be enforce or dry-run")
	}
	if c.HeartbeatInterval > 0 && c.LeaseTTL > 0 && c.LeaseTTL < c.HeartbeatInterval {
		return fmt.Errorf("BRUISER_LEASE_TTL (%s) must be >= BRUISER_HEARTBEAT_INTERVAL (%s)", c.LeaseTTL, c.HeartbeatInterval)
	}
	if c.MaxLifetime > 0 && c.LeaseTTL > 0 && c.MaxLifetime < c.LeaseTTL {
		return fmt.Errorf("BRUISER_MAX_LIFETIME must be >= BRUISER_LEASE_TTL")
	}
	if c.ProxyAddr != "" && c.OriginURL == "" {
		return fmt.Errorf("BRUISER_ORIGIN_URL is required when BRUISER_PROXY_ADDR is set")
	}
	if c.EnforcePercent > 100 {
		return fmt.Errorf("BRUISER_ENFORCE_PERCENT must be 0–100")
	}
	if err := c.ValidateSecrets(); err != nil {
		return err
	}
	if err := c.ValidateIdentity(); err != nil {
		return err
	}
	return nil
}

// ValidateIdentity is the production gate for merchant assertion mode.
func (c Config) ValidateIdentity() error {
	if !c.Production() {
		return nil
	}
	if c.DevAssertions {
		return fmt.Errorf("BRUISER_DEV_ASSERTIONS is enabled; production refuses development HMAC assertion mode")
	}
	if strings.TrimSpace(c.JWKSURL) == "" {
		return fmt.Errorf("BRUISER_JWKS_URL is required in production (asymmetric merchant identity)")
	}
	if strings.TrimSpace(c.Issuer) == "" {
		return fmt.Errorf("BRUISER_ISSUER is required in production")
	}
	if strings.TrimSpace(c.Audience) == "" {
		return fmt.Errorf("BRUISER_AUDIENCE is required in production")
	}
	return nil
}

// ValidateSecrets is the production gate for admin/edge/origin credentials.
func (c Config) ValidateSecrets() error {
	return c.validateSecrets()
}

func (c Config) validateSecrets() error {
	if strings.TrimSpace(c.AdminSecret) == "" {
		return fmt.Errorf("BRUISER_ADMIN_SECRET is required and must not be empty")
	}
	if strings.TrimSpace(c.EdgeSecret) == "" {
		return fmt.Errorf("BRUISER_EDGE_SECRET is required and must not be empty")
	}
	if c.AdminSecret == c.EdgeSecret {
		return fmt.Errorf("BRUISER_ADMIN_SECRET must be distinct from BRUISER_EDGE_SECRET")
	}
	if c.OriginSecret != "" && c.AdminSecret == c.OriginSecret {
		return fmt.Errorf("BRUISER_ADMIN_SECRET must be distinct from BRUISER_ORIGIN_SECRET")
	}
	if c.OriginSecret != "" && c.EdgeSecret == c.OriginSecret {
		return fmt.Errorf("BRUISER_EDGE_SECRET must be distinct from BRUISER_ORIGIN_SECRET")
	}
	if c.Production() {
		for _, pair := range []struct{ name, val string }{
			{"BRUISER_ADMIN_SECRET", c.AdminSecret},
			{"BRUISER_EDGE_SECRET", c.EdgeSecret},
			{"BRUISER_ORIGIN_SECRET", c.OriginSecret},
			{"BRUISER_DEV_HMAC_SECRET", c.DevHMACSecret},
		} {
			if isLabSecret(pair.val) {
				return fmt.Errorf("%s is a lab/placeholder value; production requires a dedicated secret", pair.name)
			}
		}
		if strings.TrimSpace(c.OriginSecret) == "" {
			return fmt.Errorf("BRUISER_ORIGIN_SECRET is required in production (origin lockdown)")
		}
		if isLabSecret(c.OriginSecret) {
			return fmt.Errorf("BRUISER_ORIGIN_SECRET is a lab/placeholder value; production requires a generated secret")
		}
	}
	return nil
}

func isLabSecret(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return true
	}
	switch s {
	case "change-me", "edge-secret-dev", "origin-lock-dev", "admin-secret-dev", "dev-secret-change-me":
		return true
	}
	return strings.HasPrefix(s, "change-me")
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func lookupEnv(key string) string {
	return os.Getenv(key)
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

func envBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}
