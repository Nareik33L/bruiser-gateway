package config

import (
	"strings"
	"testing"
	"time"
)

func labConfig() Config {
	c := Load()
	c.Environment = "lab"
	c.DevAssertions = true
	c.AdminSecret = "admin-secret-dev"
	c.OperatorSecret = "operator-secret-dev"
	c.EdgeSecret = "edge-secret-dev"
	c.OriginSecret = "origin-lock-dev"
	c.DevHMACSecret = "dev-secret-change-me"
	c.AdminAddr = "127.0.0.1:8082"
	c.AllowUnsafeModes = false
	c.FailClosed = true
	c.Enforcement = true
	c.Mode = "enforce"
	c.EnforcePercent = -1
	return c
}

func prodSecrets(c *Config) {
	c.AdminSecret = "prod-admin-unique"
	c.OperatorSecret = "prod-operator-unique"
	c.EdgeSecret = "prod-edge-unique"
	c.OriginSecret = "prod-origin-unique"
	c.DevHMACSecret = "prod-hmac-unique"
	c.AdminAddr = "127.0.0.1:8082"
	c.JWKSURL = "https://idp.example/.well-known/jwks.json"
	c.Issuer = "https://idp.example"
	c.Audience = "bruiser"
	c.DevAssertions = false
	c.DatabaseURL = "postgres://bruiser_app:unique-db-password@db.example.internal:5432/bruiser?sslmode=require"
	c.MerchantID = "merchant-placeholder"
	c.ProfilePath = "configs/production.example.yaml"
}

func TestProductionLoadDoesNotInventSecrets(t *testing.T) {
	t.Setenv("BRUISER_ENV", "")
	t.Setenv("BRUISER_ENVIRONMENT", "")
	t.Setenv("BRUISER_ADMIN_SECRET", "")
	t.Setenv("BRUISER_OPERATOR_SECRET", "")
	t.Setenv("BRUISER_EDGE_SECRET", "")
	t.Setenv("BRUISER_ORIGIN_SECRET", "")
	t.Setenv("BRUISER_DEV_HMAC_SECRET", "")
	c := Load()
	if c.AdminSecret != "" || c.EdgeSecret != "" || c.OriginSecret != "" || c.DevHMACSecret != "" {
		t.Fatalf("production Load invented secrets: admin=%q edge=%q origin=%q hmac=%q", c.AdminSecret, c.EdgeSecret, c.OriginSecret, c.DevHMACSecret)
	}
}

func TestLoadUnsetEnvIsProduction(t *testing.T) {
	t.Setenv("BRUISER_ENV", "")
	t.Setenv("BRUISER_ENVIRONMENT", "")
	t.Setenv("BRUISER_DEV_ASSERTIONS", "")
	c := Load()
	if !c.Production() {
		t.Fatal("unset BRUISER_ENV must be production")
	}
	if c.Lab() {
		t.Fatal("unset BRUISER_ENV must not be lab")
	}
	if c.DevAssertions {
		t.Fatal("unset BRUISER_ENV must not enable HMAC dev assertions")
	}
}

func TestUnrecognisedEnvIsProduction(t *testing.T) {
	c := labConfig()
	c.Environment = "staging"
	if !c.Production() || c.Lab() {
		t.Fatal("unrecognised BRUISER_ENV must be production")
	}
	c.Environment = "dev"
	if c.Production() || !c.Lab() {
		t.Fatal("BRUISER_ENV=dev must be lab")
	}
	c.Environment = "test"
	if c.Production() || !c.Lab() {
		t.Fatal("BRUISER_ENV=test must be lab")
	}
}

func TestValidateEnvironmentAllowsEmptyAsProduction(t *testing.T) {
	c := labConfig()
	c.Environment = ""
	c.DevAssertions = false
	prodSecrets(&c)
	if err := c.Validate(); err != nil {
		t.Fatalf("empty env is production and should validate with prod secrets: %v", err)
	}
	c.DevAssertions = true
	if err := c.Validate(); err == nil {
		t.Fatal("HMAC assertions without lab posture must fail")
	}
}

func TestProductionDefaultSecretsNameEach(t *testing.T) {
	c := Load()
	c.Environment = "production"
	c.DevAssertions = false
	c.AdminSecret = "admin-secret-dev"
	c.OperatorSecret = "operator-secret-dev"
	c.EdgeSecret = "edge-secret-dev"
	c.OriginSecret = "origin-lock-dev"
	c.DevHMACSecret = "dev-secret-change-me"
	c.AdminAddr = "127.0.0.1:8082"
	c.JWKSURL = "https://idp.example/.well-known/jwks.json"
	c.Issuer = "https://idp.example"
	c.Audience = "bruiser"
	err := c.Validate()
	if err == nil {
		t.Fatal("production + default secrets must refuse to boot")
	}
	msg := err.Error()
	for _, name := range []string{
		"BRUISER_ADMIN_SECRET",
		"BRUISER_OPERATOR_SECRET",
		"BRUISER_EDGE_SECRET",
		"BRUISER_ORIGIN_SECRET",
		"BRUISER_DEV_HMAC_SECRET",
	} {
		if !strings.Contains(msg, name) {
			t.Fatalf("error must name %s: %s", name, msg)
		}
	}
}

func TestProductionDryRunRequiresAcknowledgement(t *testing.T) {
	c := labConfig()
	c.Environment = "production"
	prodSecrets(&c)
	c.Mode = "dry-run"
	c.AllowUnsafeModes = false
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "BRUISER_MODE=dry-run") || !strings.Contains(err.Error(), "BRUISER_ALLOW_UNSAFE_MODES") {
		t.Fatalf("production dry-run without acknowledgement: %v", err)
	}
	c.AllowUnsafeModes = true
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	warns := c.UnsafeModeWarnings()
	if len(warns) == 0 || !strings.Contains(warns[0], "BRUISER_MODE=dry-run") {
		t.Fatalf("acknowledged dry-run must warn: %v", warns)
	}
	if !strings.Contains(c.StartupBanner(), "mode=dry-run") {
		t.Fatalf("banner %s", c.StartupBanner())
	}
}

func TestProductionRampAndFailOpenRequireAcknowledgement(t *testing.T) {
	c := labConfig()
	c.Environment = "production"
	prodSecrets(&c)
	c.EnforcePercent = 10
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "BRUISER_ENFORCE_PERCENT=10") {
		t.Fatalf("partial ramp: %v", err)
	}
	c.EnforcePercent = -1
	c.Enforcement = false
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "BRUISER_ENFORCEMENT=false") {
		t.Fatalf("enforcement off: %v", err)
	}
	c.Enforcement = true
	c.FailClosed = false
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "fail-open") {
		t.Fatalf("fail-open: %v", err)
	}
}

func TestProductionIdentityUnconfigured(t *testing.T) {
	c := labConfig()
	c.Environment = "production"
	prodSecrets(&c)
	c.JWKSURL = ""
	c.Issuer = ""
	c.Audience = ""
	err := c.Validate()
	if err == nil {
		t.Fatal("production without JWKS/issuer/audience must refuse")
	}
	msg := err.Error()
	for _, name := range []string{"BRUISER_JWKS_URL", "BRUISER_ISSUER", "BRUISER_AUDIENCE"} {
		if !strings.Contains(msg, name) {
			t.Fatalf("identity error must name %s: %s", name, msg)
		}
	}
}

func TestLabSecretWarningsDoNotFail(t *testing.T) {
	c := labConfig()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	warns := c.SecretWarnings()
	if len(warns) < 3 {
		t.Fatalf("lab should warn about placeholder secrets: %v", warns)
	}
}

func TestStartupBanner(t *testing.T) {
	c := labConfig()
	line := c.StartupBanner()
	if !strings.HasPrefix(line, "bruiser startup ") {
		t.Fatalf("banner %s", line)
	}
	for _, need := range []string{"env=lab", "mode=enforce", "enforce_percent=100", "fail_open=false"} {
		if !strings.Contains(line, need) {
			t.Fatalf("banner missing %s: %s", need, line)
		}
	}
}

func TestValidateLeaseCompatibility(t *testing.T) {
	c := labConfig()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.LeaseTTL = time.Second
	c.HeartbeatInterval = 5 * time.Second
	if err := c.Validate(); err == nil {
		t.Fatal("expected lease < heartbeat to fail")
	}
	c = labConfig()
	c.Mode = "observe"
	if err := c.Validate(); err == nil {
		t.Fatal("expected invalid mode to fail")
	}
	c = labConfig()
	c.ProxyAddr = ":8081"
	c.OriginURL = ""
	if err := c.Validate(); err == nil {
		t.Fatal("expected proxy without origin to fail")
	}
}

func TestValidateRejectsAdminFallback(t *testing.T) {
	c := labConfig()
	c.AdminSecret = ""
	if err := c.Validate(); err == nil {
		t.Fatal("empty admin secret must fail")
	}
	c = labConfig()
	c.AdminSecret = c.EdgeSecret
	if err := c.Validate(); err == nil {
		t.Fatal("admin == edge must fail")
	}
	c = labConfig()
	c.AdminSecret = c.OriginSecret
	if err := c.Validate(); err == nil {
		t.Fatal("admin == origin must fail")
	}
	c = labConfig()
	c.Environment = "production"
	if err := c.Validate(); err == nil {
		t.Fatal("production must reject lab secrets")
	}
	c = labConfig()
	c.Environment = "production"
	prodSecrets(&c)
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.DevAssertions = true
	if err := c.Validate(); err == nil {
		t.Fatal("production must refuse development assertions")
	}
	c.DevAssertions = false
	c.JWKSURL = ""
	if err := c.Validate(); err == nil {
		t.Fatal("production must require JWKS")
	}
}

func TestProductionRequiresAdminAddrAndDistinctOperator(t *testing.T) {
	c := labConfig()
	c.Environment = "production"
	prodSecrets(&c)
	c.AdminAddr = ""
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "BRUISER_ADMIN_ADDR") {
		t.Fatalf("missing admin addr: %v", err)
	}
	prodSecrets(&c)
	c.AdminAddr = c.HTTPAddr
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "distinct") {
		t.Fatalf("same listen: %v", err)
	}
	prodSecrets(&c)
	c.OperatorSecret = c.AdminSecret
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "BRUISER_OPERATOR_SECRET") {
		t.Fatalf("operator == admin: %v", err)
	}
	prodSecrets(&c)
	c.OperatorSecret = "operator-secret-dev"
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "BRUISER_OPERATOR_SECRET") {
		t.Fatalf("lab operator secret: %v", err)
	}
}

func TestProductionRefusesLabDatabaseURL(t *testing.T) {
	c := labConfig()
	c.Environment = "production"
	prodSecrets(&c)
	c.DatabaseURL = "postgres://bruiser:bruiser@127.0.0.1:5432/bruiser?sslmode=disable"
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "BRUISER_DATABASE_URL") {
		t.Fatalf("lab DSN must fail production: %v", err)
	}
}

func TestProductionAllowsEmptyHMAC(t *testing.T) {
	c := labConfig()
	c.Environment = "production"
	prodSecrets(&c)
	c.DevHMACSecret = ""
	if err := c.Validate(); err != nil {
		t.Fatalf("production does not use HMAC assertions: %v", err)
	}
}

func TestProductionPlaceholderHMACStillRefused(t *testing.T) {
	c := labConfig()
	c.Environment = "production"
	prodSecrets(&c)
	c.DevHMACSecret = "change-me"
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "BRUISER_DEV_HMAC_SECRET") {
		t.Fatalf("placeholder HMAC must still fail: %v", err)
	}
}

func TestOverlayProfileFillsIdentity(t *testing.T) {
	t.Setenv("BRUISER_MERCHANT_ID", "")
	t.Setenv("BRUISER_MERCHANT_NAME", "")
	c := Config{MerchantID: "arsenal", MerchantName: "Arsenal FC"}
	c.OverlayProfile("https://idp.example/jwks", "https://idp.example", "bruiser", "merchant-placeholder", "Example merchant")
	if c.JWKSURL != "https://idp.example/jwks" || c.Issuer != "https://idp.example" || c.Audience != "bruiser" {
		t.Fatalf("overlay identity: %+v", c)
	}
	if c.MerchantID != "merchant-placeholder" {
		t.Fatalf("overlay merchant: %s", c.MerchantID)
	}
	c.JWKSURL = "https://env.example/jwks"
	c.OverlayProfile("https://idp.example/jwks", "", "", "", "")
	if c.JWKSURL != "https://env.example/jwks" {
		t.Fatal("environment JWKS must win")
	}
}

func TestProductionWarnings(t *testing.T) {
	c := labConfig()
	c.Environment = "production"
	prodSecrets(&c)
	c.DatabaseURL = "postgres://bruiser_app:unique-db-password@db.example.internal:5432/bruiser?sslmode=disable"
	c.ProfilePath = "configs/arsenal.yaml"
	c.MerchantID = "arsenal"
	warns := c.ProductionWarnings()
	blob := strings.Join(warns, "; ")
	for _, need := range []string{"sslmode=disable", "arsenal.yaml", "arsenal", "BRUISER_DEV_HMAC_SECRET"} {
		if !strings.Contains(blob, need) {
			t.Fatalf("warnings missing %q: %v", need, warns)
		}
	}
}

func TestShippedPlaceholdersFailProduction(t *testing.T) {
	for _, secret := range []string{"", "change-me", "change-me-admin", "change-me-edge"} {
		if !isLabSecret(secret) {
			t.Fatalf("%q must fail production secret validation", secret)
		}
	}
	c := labConfig()
	c.Environment = "production"
	c.DevAssertions = false
	c.AdminSecret = "change-me"
	c.OperatorSecret = "change-me-operator"
	c.EdgeSecret = "change-me-edge"
	c.OriginSecret = "change-me-origin"
	c.DevHMACSecret = "change-me"
	c.AdminAddr = "127.0.0.1:8082"
	c.JWKSURL = "https://idp.example/.well-known/jwks.json"
	c.Issuer = "https://idp.example"
	c.Audience = "bruiser"
	err := c.Validate()
	if err == nil {
		t.Fatal("shipped change-me placeholders must refuse production boot")
	}
}
