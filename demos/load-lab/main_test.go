package main

import (
	"net/http"
	"testing"

	"github.com/Nareik33L/bruiser-gateway/demos/seed"
)

func TestClassifyHold(t *testing.T) {
	cases := []struct {
		decision string
		status   int
		want     string
	}{
		{"BUSY", http.StatusConflict, "busy"},
		{"ALLOW", http.StatusCreated, "allow"},
		{"ALLOW", http.StatusConflict, "denied"}, // Off + origin per-account / sold out
		{"", http.StatusConflict, "denied"},
		{"DENIED", http.StatusForbidden, "denied"},
		{"", http.StatusCreated, "allow"},
	}
	for _, tc := range cases {
		if got := classifyHold(tc.decision, tc.status); got != tc.want {
			t.Fatalf("classifyHold(%q,%d)=%s want %s", tc.decision, tc.status, got, tc.want)
		}
	}
}

func TestMembershipForTenRoundRobin(t *testing.T) {
	ids := seed.TenMemberships()
	req := startReq{Preset: "ten", Membership: "9999999"}
	seen := map[string]bool{}
	for i := 0; i < 10; i++ {
		got := membershipFor(req, i, ids, nil)
		if got == req.Membership {
			t.Fatal("ten preset must not use the single membership field")
		}
		seen[got] = true
	}
	if len(seen) != 10 {
		t.Fatalf("first 10 agents should be 10 memberships, got %d", len(seen))
	}
}
