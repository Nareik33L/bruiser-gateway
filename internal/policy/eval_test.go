package policy

import (
	"strings"
	"testing"
	"time"
)

func TestEvaluateDimensions(t *testing.T) {
	doc := Document{
		Version:  1,
		Merchant: "arsenal",
		Defaults: Defaults{LeaseTTL: "60s", MaxLifetime: "15m"},
		Domains: []Rule{
			{
				Name:            "household-cap",
				Match:           Match{Action: []string{"purchase", "hold"}, ResourcePrefix: "event:"},
				Scope:           []string{"anchor:household_id", "resource"},
				MaxActive:       1,
				OnMissingAnchor: MissingDeny,
				Precedence:      []string{"browser", "agent"},
			},
			{
				Name:       "per-principal-type",
				Match:      Match{Action: []string{"hold"}},
				Scope:      []string{"customer", "principal_type", "resource_pool"},
				MaxActive:  2,
				LeaseTTL:   "30s",
				Precedence: []string{"agent", "browser"},
			},
			{
				Name:      "purchase-per-event",
				Match:     Match{Action: []string{"purchase", "hold"}},
				Scope:     []string{"customer", "resource"},
				MaxActive: 1,
			},
			{
				Name:    "discovery",
				Match:   Match{Action: []string{"search"}},
				Control: ControlNone,
			},
		},
		Fallback: FallbackDeny,
	}
	c, err := Compile(doc)
	if err != nil {
		t.Fatal(err)
	}

	base := Request{
		MerchantID: "arsenal", CustomerID: "alice", PrincipalType: "agent",
		Resource: "event:ars-che", Action: "hold",
		Anchors: map[string]string{"household_id": "hh-1"},
	}

	tests := []struct {
		name string
		req  Request
		want func(*testing.T, Decision)
	}{
		{
			name: "household first-match",
			req:  base,
			want: func(t *testing.T, d Decision) {
				if d.Denied || d.RuleName != "household-cap" {
					t.Fatalf("%+v", d)
				}
				if !strings.Contains(d.DomainKey, "anchor:household_id=hh-1") {
					t.Fatalf("domain %s", d.DomainKey)
				}
				if !strings.Contains(d.DomainKey, "resource=event:ars-che") {
					t.Fatalf("domain %s", d.DomainKey)
				}
			},
		},
		{
			name: "missing household denies",
			req: func() Request {
				r := base
				r.Anchors = nil
				return r
			}(),
			want: func(t *testing.T, d Decision) {
				if !d.Denied || d.Reason != "missing_anchor" {
					t.Fatalf("%+v", d)
				}
			},
		},
		{
			name: "search uncontrolled",
			req: Request{
				MerchantID: "arsenal", Action: "search", Resource: "event:ars-che",
			},
			want: func(t *testing.T, d Decision) {
				if !d.ControlNone || d.RuleName != "discovery" {
					t.Fatalf("%+v", d)
				}
			},
		},
		{
			name: "fallback deny",
			req: Request{
				MerchantID: "arsenal", Action: "cancel", Resource: "event:x", CustomerID: "alice",
			},
			want: func(t *testing.T, d Decision) {
				if !d.Denied || d.Reason != "no_matching_rule" {
					t.Fatalf("%+v", d)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.want(t, c.Evaluate(tc.req))
		})
	}

	fall, err := Compile(Document{
		Version: 1,
		Domains: []Rule{{
			Name: "only-search", Match: Match{Action: []string{"search"}}, Control: ControlNone,
		}},
		Fallback: FallbackAllowUncontrolled,
	})
	if err != nil {
		t.Fatal(err)
	}
	d := fall.Evaluate(Request{Action: "purchase", Resource: "event:x"})
	if !d.ControlNone {
		t.Fatalf("fallback allow: %+v", d)
	}
}

func TestFallthroughMissingAnchor(t *testing.T) {
	c, err := Compile(Document{
		Version: 1,
		Domains: []Rule{
			{
				Name:            "household-cap",
				Match:           Match{Action: []string{"purchase"}},
				Scope:           []string{"anchor:household_id", "resource"},
				OnMissingAnchor: MissingFallthrough,
			},
			{
				Name:      "purchase-per-event",
				Match:     Match{Action: []string{"purchase"}},
				Scope:     []string{"customer", "resource"},
				MaxActive: 1,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	d := c.Evaluate(Request{
		MerchantID: "m", CustomerID: "alice", Resource: "event:x", Action: "purchase",
	})
	if d.Denied || d.RuleName != "purchase-per-event" {
		t.Fatalf("%+v", d)
	}
	if d.TTL != 60*time.Second || d.MaxLifetime != 15*time.Minute {
		t.Fatalf("defaults ttl=%s life=%s", d.TTL, d.MaxLifetime)
	}
}

func TestPrincipalTypeAndPool(t *testing.T) {
	c, err := Compile(Document{
		Version: 1,
		Domains: []Rule{{
			Name:      "pool",
			Match:     Match{Action: []string{"hold"}},
			Scope:     []string{"customer", "principal_type", "resource_pool"},
			MaxActive: 3,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	d := c.Evaluate(Request{
		MerchantID: "m", CustomerID: "alice", PrincipalType: "agent",
		Resource: "event:ars-che", Action: "hold",
	})
	want := "m/pool/customer=alice/principal_type=agent/resource_pool=event"
	if d.DomainKey != want {
		t.Fatalf("got %s want %s", d.DomainKey, want)
	}
	if d.MaxActive != 3 {
		t.Fatalf("max_active %d", d.MaxActive)
	}
}

func TestCompileRejectsUnknownDim(t *testing.T) {
	_, err := Compile(Document{
		Version: 1,
		Domains: []Rule{{Name: "x", Scope: []string{"nope"}}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestWaitingBoundedCompiles(t *testing.T) {
	c, err := Compile(Document{
		Version: 1,
		Domains: []Rule{{
			Name:      "purchase-per-event",
			Match:     Match{Action: []string{"purchase"}},
			Scope:     []string{"customer", "resource"},
			MaxActive: 1,
			Waiting:   Waiting{Mode: "bounded", MaxWaiters: 2},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	d := c.Evaluate(Request{
		MerchantID: "m", CustomerID: "alice", Resource: "event:x", Action: "purchase",
	})
	if d.MaxWaiters != 2 {
		t.Fatalf("max_waiters=%d", d.MaxWaiters)
	}
	none, err := Compile(Document{
		Version: 1,
		Domains: []Rule{{
			Name: "x", Match: Match{Action: []string{"purchase"}},
			Scope: []string{"customer", "resource"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if none.Evaluate(Request{MerchantID: "m", CustomerID: "a", Resource: "r", Action: "purchase"}).MaxWaiters != 0 {
		t.Fatal("default waiting should be none")
	}
}

func TestDefaultDocumentCompiles(t *testing.T) {
	c, err := Compile(DefaultDocument("arsenal"))
	if err != nil {
		t.Fatal(err)
	}
	d := c.Evaluate(Request{
		MerchantID: "arsenal", CustomerID: "1001234", PrincipalType: "agent",
		Resource: "event:ars-che", Action: "purchase",
	})
	if d.Denied || d.RuleName != "purchase-per-event" {
		t.Fatalf("%+v", d)
	}
}
