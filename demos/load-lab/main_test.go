package main

import (
	"net/http"
	"testing"

	"github.com/Nareik33L/bruiser-gateway/demos/seed"
)

func TestRewriteHandoffURL(t *testing.T) {
	cases := []struct {
		name, loc, base, want string
	}{
		{
			"compose public host",
			"http://tickets.localhost:8091/sso?token=abc",
			"http://simtix:8091",
			"http://simtix:8091/sso?token=abc",
		},
		{
			"local loopback",
			"http://tickets.localhost:8091/sso?token=abc",
			"http://127.0.0.1:8091",
			"http://127.0.0.1:8091/sso?token=abc",
		},
		{
			"https public to http edge",
			"https://tickets.bruiser-gateway.com/sso?token=z",
			"http://simtix:8091",
			"http://simtix:8091/sso?token=z",
		},
		{
			"relative location",
			"/sso?token=rel",
			"http://simtix:8091",
			"http://simtix:8091/sso?token=rel",
		},
		{
			"already edge host",
			"http://127.0.0.1:8091/sso?token=same",
			"http://127.0.0.1:8091",
			"http://127.0.0.1:8091/sso?token=same",
		},
		{
			"keeps extra query",
			"http://tickets.localhost:8091/sso?token=a&next=/x",
			"http://simtix:8091",
			"http://simtix:8091/sso?token=a&next=/x",
		},
		{"empty loc", "", "http://simtix:8091", ""},
		{"empty base leaves loc", "http://tickets.localhost:8091/sso?t=1", "", "http://tickets.localhost:8091/sso?t=1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rewriteHandoffURL(tc.loc, tc.base); got != tc.want {
				t.Fatalf("rewriteHandoffURL(%q,%q)=%q want %q", tc.loc, tc.base, got, tc.want)
			}
		})
	}
}

func TestClearEmptiesEvents(t *testing.T) {
	l := &lab{}
	l.run = &run{
		cancel: func() {},
		events: []event{{Type: "agent", ID: 1, Status: "denied", Detail: "sso-error"}},
		sum:    Summary{Denied: 50, Authenticated: 50},
		preset: "single",
	}
	l.running = true
	l.clear()
	if l.running {
		t.Fatal("running should be false after clear")
	}
	if !l.run.cleared || len(l.run.events) != 0 {
		t.Fatalf("events should be empty and cleared, got %+v", l.run)
	}
	if l.run.sum.Denied != 0 || l.run.sum.Authenticated != 0 {
		t.Fatalf("summary should be zero, got %+v", l.run.sum)
	}
}

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
