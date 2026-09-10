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
	OperatorSecret    string
	AdminAddr         string
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
	FailClosed        bool
	AllowUnsafeModes  bool
}

func Load() Config {
	c := Config{
		HTTPAddr:          env("BRUISER_HTTP_ADDR", ":8080"),
		DatabaseURL:       env("BRUISER_DATABASE_URL", "postgres://bruiser:bruiser@127.0.0.1:5432/bruiser?sslmode=disable"),
		DevHMACSecret:     lookupEnv("BRUISER_DEV_HMAC_SECRET"),
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
		EdgeSecret:        lookupEnv("BRUISER_EDGE_SECRET"),
		OriginSecret:      lookupEnv("BRUISER_ORIGIN_SECRET"),
		ProfilePath:       env("BRUISER_PROFILE", "configs/arsenal.yaml"),
		ProxyAddr:         env("BRUISER_PROXY_ADDR", ""),
		OriginURL:         env("BRUISER_ORIGIN_URL", ""),
		MaxInFlight:       envInt("BRUISER_MAX_IN_FLIGHT", 2),
		RatePerSec:        envFloat("BRUISER_RATE_PER_SEC", 5),
		AuditRetention:    envDuration("BRUISER_AUDIT_RETENTION", 13*30*24*time.Hour),
		CheckEdgeURL:      env("BRUISER_CHECK_EDGE_URL", "http://127.0.0.1:8091"),
		CheckOriginURL:    env("BRUISER_CHECK_ORIGIN_URL", "http://127.0.0.1:8090"),
		AdminSecret:       lookupEnv("BRUISER_ADMIN_SECRET"),
		OperatorSecret:    lookupEnv("BRUISER_OPERATOR_SECRET"),
		AdminAddr:         lookupEnv("BRUISER_ADMIN_ADDR"),
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
		FailClosed:        envBool("BRUISER_FAIL_CLOSED", true) && !envBool("BRUISER_FAIL_OPEN", false),
		AllowUnsafeModes:  envBool("BRUISER_ALLOW_UNSAFE_MODES", false),
	}
	// HMAC dev assertions stay off unless the operator sets the flag.
	// Validate refuses the flag outside explicit lab/dev/test.
	c.DevAssertions = envBool("BRUISER_DEV_ASSERTIONS", false)
	c.NormalizeListen()
	return c
}

// NormalizeListen fills lab-only listen/credential defaults. Production
// never invents an admin bind address or operator secret.
func (c *Config) NormalizeListen() {
	if c == nil || !c.Lab() {
		return
	}
	if strings.TrimSpace(c.AdminAddr) == "" {
		c.AdminAddr = "127.0.0.1:8082"
	}
	if strings.TrimSpace(c.OperatorSecret) == "" {
		c.OperatorSecret = "operator-secret-dev"
	}
	if strings.TrimSpace(c.AdminSecret) == "" {
		c.AdminSecret = "admin-secret-dev"
	}
	if strings.TrimSpace(c.EdgeSecret) == "" {
		c.EdgeSecret = "edge-secret-dev"
	}
	if strings.TrimSpace(c.OriginSecret) == "" {
		c.OriginSecret = "origin-lock-dev"
	}
	if strings.TrimSpace(c.DevHMACSecret) == "" {
		c.DevHMACSecret = "dev-secret-change-me"
	}
}

// Lab is true only when the operator asked for lab/dev/test. Unset and
// unrecognised BRUISER_ENV values are production.
func (c Config) Lab() bool {
	switch strings.ToLower(strings.TrimSpace(c.Environment)) {
	case "lab", "dev", "test":
		return true
	default:
		return false
	}
}

// Production is the fail-closed default: everything that is not an
// explicit lab posture is production.
func (c Config) Production() bool {
	return !c.Lab()
}

func (c Config) Validate() error {
	if err := c.ValidateEnvironment(); err != nil {
		return err
	}
	if c.DatabaseURL == "" {
		return fmt.Errorf("BRUISER_DATABASE_URL is required")
	}
	if c.DevHMACSecret == "" && c.Lab() {
		return fmt.Errorf("BRUISER_DEV_HMAC_SECRET is required in lab (development HMAC assertions)")
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
	if err := c.ValidateAdminListen(); err != nil {
		return err
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
	if err := c.ValidateUnsafeModes(); err != nil {
		return err
	}
	return nil
}

// ValidateEnvironment allows unset/unknown values (they are production).
// HMAC assertions are lab-only.
func (c Config) ValidateEnvironment() error {
	if c.DevAssertions && !c.Lab() {
		return fmt.Errorf("BRUISER_DEV_ASSERTIONS requires BRUISER_ENV=lab (or dev/test); HMAC lab mode is not silent")
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
	var missing []string
	if strings.TrimSpace(c.JWKSURL) == "" {
		missing = append(missing, "BRUISER_JWKS_URL")
	}
	if strings.TrimSpace(c.Issuer) == "" {
		missing = append(missing, "BRUISER_ISSUER")
	}
	if strings.TrimSpace(c.Audience) == "" {
		missing = append(missing, "BRUISER_AUDIENCE")
	}
	if len(missing) > 0 {
		return fmt.Errorf("production identity is unconfigured; required: %s", strings.Join(missing, ", "))
	}
	return nil
}

// ValidateSecrets always checks presence and distinctness. Production
// hard-fails lab/placeholder secrets; lab callers should log SecretWarnings.
func (c Config) ValidateSecrets() error {
	var problems []string
	if strings.TrimSpace(c.AdminSecret) == "" {
		problems = append(problems, "BRUISER_ADMIN_SECRET is required and must not be empty")
	}
	if strings.TrimSpace(c.OperatorSecret) == "" {
		problems = append(problems, "BRUISER_OPERATOR_SECRET is required and must not be empty")
	}
	if strings.TrimSpace(c.EdgeSecret) == "" {
		problems = append(problems, "BRUISER_EDGE_SECRET is required and must not be empty")
	}
	if c.AdminSecret != "" && c.AdminSecret == c.EdgeSecret {
		problems = append(problems, "BRUISER_ADMIN_SECRET must be distinct from BRUISER_EDGE_SECRET")
	}
	if c.OriginSecret != "" && c.AdminSecret == c.OriginSecret {
		problems = append(problems, "BRUISER_ADMIN_SECRET must be distinct from BRUISER_ORIGIN_SECRET")
	}
	if c.OriginSecret != "" && c.EdgeSecret == c.OriginSecret {
		problems = append(problems, "BRUISER_EDGE_SECRET must be distinct from BRUISER_ORIGIN_SECRET")
	}
	if c.OperatorSecret != "" && c.OperatorSecret == c.AdminSecret {
		problems = append(problems, "BRUISER_OPERATOR_SECRET must be distinct from BRUISER_ADMIN_SECRET")
	}
	if c.OperatorSecret != "" && c.OperatorSecret == c.EdgeSecret {
		problems = append(problems, "BRUISER_OPERATOR_SECRET must be distinct from BRUISER_EDGE_SECRET")
	}
	if c.OperatorSecret != "" && c.OriginSecret != "" && c.OperatorSecret == c.OriginSecret {
		problems = append(problems, "BRUISER_OPERATOR_SECRET must be distinct from BRUISER_ORIGIN_SECRET")
	}
	if c.Production() {
		if hits := c.labSecretHits(); len(hits) > 0 {
			problems = append(problems, "production refuses lab/placeholder secrets: "+strings.Join(hits, ", "))
		}
		if isLabDatabaseURL(c.DatabaseURL) {
			problems = append(problems, "production refuses the lab database URL (bruiser:bruiser@…); set BRUISER_DATABASE_URL to the merchant Postgres DSN")
		}
		if strings.TrimSpace(c.OriginSecret) == "" {
			problems = append(problems, "BRUISER_ORIGIN_SECRET is required in production (origin lockdown)")
		}
		if strings.TrimSpace(c.AdminAddr) == "" {
			problems = append(problems, "BRUISER_ADMIN_ADDR is required in production (admin listener must not share the public address)")
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s", strings.Join(problems, "; "))
	}
	return nil
}

// SecretWarnings lists lab/placeholder secrets. Production returns nil
// because those are hard errors from ValidateSecrets.
func (c Config) SecretWarnings() []string {
	if c.Production() {
		return nil
	}
	var out []string
	for _, name := range c.labSecretHits() {
		out = append(out, name+" is a lab/placeholder value")
	}
	return out
}

// ValidateUnsafeModes refuses dry-run, partial ramp, and fail-open in
// production unless BRUISER_ALLOW_UNSAFE_MODES=1.
func (c Config) ValidateUnsafeModes() error {
	if !c.Production() {
		return nil
	}
	reasons := c.unsafeReasons()
	if len(reasons) == 0 {
		return nil
	}
	if !c.AllowUnsafeModes {
		return fmt.Errorf("production refuses %s without BRUISER_ALLOW_UNSAFE_MODES=1", strings.Join(reasons, ", "))
	}
	return nil
}

// UnsafeModeWarnings is the prominent log when production acknowledged
// dry-run / ramp / fail-open via BRUISER_ALLOW_UNSAFE_MODES.
func (c Config) UnsafeModeWarnings() []string {
	if !c.Production() || !c.AllowUnsafeModes {
		return nil
	}
	reasons := c.unsafeReasons()
	if len(reasons) == 0 {
		return nil
	}
	return []string{"BRUISER_ALLOW_UNSAFE_MODES=1 acknowledged: " + strings.Join(reasons, ", ")}
}

func (c Config) unsafeReasons() []string {
	var reasons []string
	if strings.EqualFold(strings.TrimSpace(c.Mode), "dry-run") {
		reasons = append(reasons, "BRUISER_MODE=dry-run")
	}
	if c.EnforcePercent >= 0 && c.EnforcePercent < 100 {
		reasons = append(reasons, fmt.Sprintf("BRUISER_ENFORCE_PERCENT=%d", c.EnforcePercent))
	}
	if !c.Enforcement {
		reasons = append(reasons, "BRUISER_ENFORCEMENT=false")
	}
	if !c.FailClosed {
		reasons = append(reasons, "fail-open")
	}
	return reasons
}

func (c Config) labSecretHits() []string {
	var names []string
	for _, pair := range []struct{ name, val string }{
		{"BRUISER_ADMIN_SECRET", c.AdminSecret},
		{"BRUISER_OPERATOR_SECRET", c.OperatorSecret},
		{"BRUISER_EDGE_SECRET", c.EdgeSecret},
		{"BRUISER_ORIGIN_SECRET", c.OriginSecret},
		{"BRUISER_DEV_HMAC_SECRET", c.DevHMACSecret},
	} {
		if pair.name == "BRUISER_DEV_HMAC_SECRET" && strings.TrimSpace(pair.val) == "" && c.Production() {
			// Production does not use HMAC assertions; an unset secret is not a placeholder.
			continue
		}
		if isLabSecret(pair.val) {
			names = append(names, pair.name)
		}
	}
	return names
}

// OverlayProfile fills identity and merchant fields from the merchant
// profile when the corresponding environment variables were left empty.
// Environment values always win.
func (c *Config) OverlayProfile(jwksURL, issuer, audience, merchantID, merchantName string) {
	if c == nil {
		return
	}
	if strings.TrimSpace(c.JWKSURL) == "" {
		c.JWKSURL = strings.TrimSpace(jwksURL)
	}
	if strings.TrimSpace(c.Issuer) == "" {
		c.Issuer = strings.TrimSpace(issuer)
	}
	if strings.TrimSpace(c.Audience) == "" {
		c.Audience = strings.TrimSpace(audience)
	}
	if os.Getenv("BRUISER_MERCHANT_ID") == "" && strings.TrimSpace(merchantID) != "" {
		c.MerchantID = strings.TrimSpace(merchantID)
	}
	if os.Getenv("BRUISER_MERCHANT_NAME") == "" && strings.TrimSpace(merchantName) != "" {
		c.MerchantName = strings.TrimSpace(merchantName)
	}
}

// ProductionWarnings are operator-visible production nits that do not
// fail boot. Catalogue emptiness is reported by the profile validator.
func (c Config) ProductionWarnings() []string {
	if !c.Production() {
		return nil
	}
	var out []string
	if strings.Contains(strings.ToLower(c.DatabaseURL), "sslmode=disable") {
		out = append(out, "BRUISER_DATABASE_URL uses sslmode=disable; require TLS unless the database network is otherwise isolated")
	}
	if strings.Contains(c.ProfilePath, "arsenal.yaml") {
		out = append(out, "BRUISER_PROFILE points at the lab Arsenal analogue ("+c.ProfilePath+"); set the merchant production profile")
	}
	if strings.EqualFold(strings.TrimSpace(c.MerchantID), "arsenal") {
		out = append(out, "merchant_id is the lab default 'arsenal'; set the merchant identifier")
	}
	if strings.TrimSpace(c.DevHMACSecret) != "" {
		out = append(out, "BRUISER_DEV_HMAC_SECRET is set but production refuses HMAC assertions; omit it unless Authority Check needs a throwaway value to prove rejection")
	}
	return out
}

func isLabDatabaseURL(s string) bool {
	u := strings.ToLower(strings.TrimSpace(s))
	if u == "" {
		return false
	}
	return strings.Contains(u, "bruiser:bruiser@")
}

// ValidateAdminListen refuses a missing production admin bind and
// refuses binding admin on the same address as public traffic.
func (c Config) ValidateAdminListen() error {
	if strings.TrimSpace(c.AdminAddr) == "" {
		if c.Production() {
			return fmt.Errorf("BRUISER_ADMIN_ADDR is required in production (admin listener must not share the public address)")
		}
		return nil
	}
	if sameListenAddr(c.HTTPAddr, c.AdminAddr) {
		return fmt.Errorf("BRUISER_ADMIN_ADDR (%s) must be distinct from BRUISER_HTTP_ADDR (%s)", c.AdminAddr, c.HTTPAddr)
	}
	return nil
}

func sameListenAddr(a, b string) bool {
	return canonListen(a) != "" && canonListen(a) == canonListen(b)
}

func canonListen(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	if !strings.Contains(s, ":") {
		s = s + ":80"
	}
	if strings.HasPrefix(s, ":") {
		return "0.0.0.0" + s
	}
	if strings.HasPrefix(s, "*:") {
		return "0.0.0.0" + s[1:]
	}
	return s
}

func (c Config) IdentityMode() string {
	if c.DevAssertions {
		return "hmac-dev-assertions"
	}
	if strings.TrimSpace(c.JWKSURL) != "" {
		return "jwks"
	}
	return "unconfigured"
}

func (c Config) effectiveMode() string {
	if strings.EqualFold(strings.TrimSpace(c.Mode), "dry-run") {
		return "dry-run"
	}
	return "enforce"
}

func (c Config) effectivePercent() int {
	if strings.EqualFold(strings.TrimSpace(c.Mode), "dry-run") || !c.Enforcement {
		return 0
	}
	if c.EnforcePercent < 0 {
		return 100
	}
	return c.EnforcePercent
}

func (c Config) failOpen() bool {
	return !c.FailClosed || !c.Enforcement
}

func (c Config) envName() string {
	if c.Lab() {
		if e := strings.ToLower(strings.TrimSpace(c.Environment)); e != "" {
			return e
		}
		return "lab"
	}
	if e := strings.ToLower(strings.TrimSpace(c.Environment)); e != "" {
		return e
	}
	return "production"
}

// StartupBanner is one operator-visible line: effective mode, enforce
// percent, fail-open posture, and identity mode.
func (c Config) StartupBanner() string {
	return fmt.Sprintf("bruiser startup env=%s mode=%s enforce_percent=%d fail_open=%t identity=%s",
		c.envName(), c.effectiveMode(), c.effectivePercent(), c.failOpen(), c.IdentityMode())
}

func isLabSecret(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return true
	}
	switch s {
	case "change-me", "edge-secret-dev", "origin-lock-dev", "admin-secret-dev",
		"operator-secret-dev", "dev-secret-change-me", "jwt-secret-dev",
		"signing-key-dev", "hmac-secret-dev":
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
