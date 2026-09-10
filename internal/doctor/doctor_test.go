package doctor

import (
	"strings"
	"testing"

	"github.com/Nareik33L/bruiser-gateway/internal/config"
	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
)

func TestDoctorExampleProfile(t *testing.T) {
	cfg := config.Load()
	cfg.Environment = "lab"
	cfg.DevAssertions = true
	cfg.AdminSecret = "admin-secret-dev"
	cfg.OperatorSecret = "operator-secret-dev"
	cfg.AdminAddr = "127.0.0.1:8082"
	cfg.EdgeSecret = "edge-secret-dev"
	cfg.OriginSecret = "origin-lock-dev"
	cfg.DevHMACSecret = "dev-secret-change-me"
	p, err := merchant.LoadFile("../../configs/example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	rep := Run(Input{Config: cfg, Profile: p})
	if rep.Overall == Fail {
		t.Fatalf("example.yaml doctor should not FAIL without store probe:\n%s", rep)
	}
	var names []string
	for _, c := range rep.Checks {
		names = append(names, c.Name)
		if c.Name == "protected routes" && c.Status != Pass {
			t.Fatalf("routes: %s %s", c.Status, c.Detail)
		}
		if c.Name == "persistence" && c.Status != Warn {
			t.Fatalf("persistence without probe should WARN, got %s", c.Status)
		}
	}
	joined := strings.Join(names, ",")
	for _, need := range []string{"configuration", "identity extraction", "protected routes", "unmatched safety", "signing keys", "persistence", "queue configuration", "authority enforcement", "metrics", "audit configuration"} {
		if !strings.Contains(joined, need) {
			t.Fatalf("missing check %s in %s", need, joined)
		}
	}
}

func TestDoctorMissingRoutesFail(t *testing.T) {
	cfg := config.Load()
	cfg.Environment = "lab"
	cfg.DevAssertions = true
	cfg.AdminSecret = "admin-secret-dev"
	cfg.OperatorSecret = "operator-secret-dev"
	cfg.AdminAddr = "127.0.0.1:8082"
	cfg.EdgeSecret = "edge-secret-dev"
	cfg.DevHMACSecret = "dev-secret-change-me"
	p := merchant.Empty("x")
	rep := Run(Input{Config: cfg, Profile: p})
	if rep.Overall != Fail {
		t.Fatalf("empty profile should FAIL: %s", rep)
	}
}
