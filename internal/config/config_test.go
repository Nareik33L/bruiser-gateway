package config

import (
	"testing"
	"time"
)

func labConfig() Config {
	c := Load()
	c.AdminSecret = "admin-secret-dev"
	c.EdgeSecret = "edge-secret-dev"
	c.OriginSecret = "origin-lock-dev"
	return c
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
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}
