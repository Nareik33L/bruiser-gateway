package config

import (
	"testing"
	"time"
)

func TestValidateLeaseCompatibility(t *testing.T) {
	c := Load()
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.LeaseTTL = time.Second
	c.HeartbeatInterval = 5 * time.Second
	if err := c.Validate(); err == nil {
		t.Fatal("expected lease < heartbeat to fail")
	}
	c = Load()
	c.Mode = "observe"
	if err := c.Validate(); err == nil {
		t.Fatal("expected invalid mode to fail")
	}
	c = Load()
	c.ProxyAddr = ":8081"
	c.OriginURL = ""
	if err := c.Validate(); err == nil {
		t.Fatal("expected proxy without origin to fail")
	}
}
