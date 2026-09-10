package merchant

import (
	"strings"
	"testing"
)

func TestExampleProfileValidates(t *testing.T) {
	p, err := LoadFile("../../configs/example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestProductionExampleProfileValidates(t *testing.T) {
	p, err := LoadFile("../../configs/production.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(p.Resources) == 0 {
		t.Fatal("production example must ship a closed catalogue")
	}
	issues := p.SafetyIssues(SafetyOpts{Production: true})
	for _, i := range issues {
		if i.Level == "FAIL" {
			t.Fatalf("production example must pass SafetyIssues: %+v", issues)
		}
	}
}

func TestValidateMissingRoutes(t *testing.T) {
	p := Empty("x")
	if err := p.Validate(); err == nil {
		t.Fatal("expected missing allocation routes to fail")
	}
}

func TestUnmatchedAllowWithoutLockdownFails(t *testing.T) {
	p, err := Parse([]byte(`
merchant_id: example
unmatched: allow
identity:
  extractor: auto
  header: X-Customer-Id
routes:
  - match: { method: POST, path: "/api/holds" }
    resource: "sku:one"
    action: hold
`))
	if err != nil {
		t.Fatal(err)
	}
	issues := p.SafetyIssues(SafetyOpts{OriginSecret: ""})
	fail := false
	for _, i := range issues {
		if i.Level == "FAIL" && i.Field == "unmatched" {
			fail = true
		}
	}
	if !fail {
		t.Fatalf("unmatched allow without origin secret must FAIL: %+v", issues)
	}
	issues = p.SafetyIssues(SafetyOpts{OriginSecret: "lock"})
	fail = false
	for _, i := range issues {
		if i.Level == "FAIL" {
			fail = true
		}
	}
	if fail {
		t.Fatalf("lockdown on should not FAIL: %+v", issues)
	}
}

func TestProductionRejectsAutoUnsignedHeader(t *testing.T) {
	p, err := LoadFile("../../configs/example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	issues := p.SafetyIssues(SafetyOpts{
		OriginSecret: "prod-origin-unique",
		Production:   true,
		JWKSURL:      "https://idp.example/.well-known/jwks.json",
		Issuer:       "https://idp.example",
		Audience:     "bruiser",
	})
	found := false
	for _, i := range issues {
		if i.Level == "FAIL" && i.Field == "identity.header" {
			found = true
		}
	}
	if !found {
		t.Fatalf("production auto+header must FAIL without opt-in: %+v", issues)
	}
	p.Identity.AllowUnsignedHeader = true
	issues = p.SafetyIssues(SafetyOpts{
		OriginSecret: "prod-origin-unique",
		Production:   true,
		JWKSURL:      "https://idp.example/.well-known/jwks.json",
		Issuer:       "https://idp.example",
		Audience:     "bruiser",
	})
	for _, i := range issues {
		if i.Level == "FAIL" && strings.HasPrefix(i.Field, "identity") {
			t.Fatalf("opt-in should allow unsigned header: %+v", issues)
		}
	}
}

func TestLooksLikeAllocationWithoutResourceFails(t *testing.T) {
	p, err := Parse([]byte(`
merchant_id: x
identity: { extractor: cookie-jwt, cookie: s }
routes:
  - match: { method: POST, path: "/api/tickets/hold" }
    action: hold
`))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(); err == nil {
		t.Fatal("hold path without resource must FAIL")
	}
}
