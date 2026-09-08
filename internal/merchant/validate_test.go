package merchant

import "testing"

func TestExampleProfileValidates(t *testing.T) {
	p, err := LoadFile("../../configs/example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestValidateMissingRoutes(t *testing.T) {
	p := Empty("x")
	if err := p.Validate(); err == nil {
		t.Fatal("expected missing allocation routes to fail")
	}
}

func TestUnmatchedAllowWithoutLockdownFails(t *testing.T) {
	p, err := LoadFile("../../configs/example.yaml")
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
