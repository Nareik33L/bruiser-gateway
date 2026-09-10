package config

import (
	"testing"
	"time"
)

func labConfig() Config {
	c := Load()
	c.Environment = "lab"
	c.DevAssertions = true
	c.AdminSecret = "admin-secret-dev"
	c.EdgeSecret = "edge-secret-dev"
	c.OriginSecret = "origin-lock-dev"
	return c
}

func TestValidateRequiresExplicitEnvironment(t *testing.T) {
	c := labConfig()
	c.Environment = ""
	if err := c.Validate(); err == nil {
		t.Fatal("empty BRUISER_ENV must fail")
	}
	c.Environment = "staging"
	if err := c.Validate(); err == nil {
		t.Fatal("unknown BRUISER_ENV must fail")
	}
	c.Environment = "production"
	c.DevAssertions = true
	c.AdminSecret = "prod-admin-unique"
	c.EdgeSecret = "prod-edge-unique"
	c.OriginSecret = "prod-origin-unique"
	c.DevHMACSecret = "prod-hmac-unique"
	c.JWKSURL = "https://idp.example/.well-known/jwks.json"
	c.Issuer = "https://idp.example"
	c.Audience = "bruiser"
	if err := c.Validate(); err == nil {
		t.Fatal("HMAC assertions without lab posture must fail")
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
	c.AdminSecret = "prod-admin-unique"
	c.EdgeSecret = "prod-edge-unique"
	c.OriginSecret = "prod-origin-unique"
	c.DevHMACSecret = "prod-hmac-unique"
	c.DevAssertions = false
	c.JWKSURL = "https://idp.example/.well-known/jwks.json"
	c.Issuer = "https://idp.example"
	c.Audience = "bruiser"
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
