package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/id"
	"github.com/Nareik33L/bruiser-gateway/internal/lease"
	"github.com/Nareik33L/bruiser-gateway/internal/ops"
)

func TestShadowDecideIntraCustomer(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	if err := s.EnsureMerchant(ctx, m, "test", "secret"); err != nil {
		t.Fatal(err)
	}
	alice := lease.DomainKey(m, "purchase-per-event", "alice", "event:x")
	bob := lease.DomainKey(m, "purchase-per-event", "bob", "event:x")
	ttl := time.Minute

	a1, err := s.ShadowDecide(ctx, m, alice, "alice", "p1", "event:x", "purchase", "purchase-per-event", 1, 1, ttl, "r1")
	if err != nil || a1.Would != "ALLOW" {
		t.Fatalf("alice1 %+v err=%v", a1, err)
	}
	a2, err := s.ShadowDecide(ctx, m, alice, "alice", "p2", "event:x", "purchase", "purchase-per-event", 1, 1, ttl, "r2")
	if err != nil || a2.Would != "QUEUE" {
		t.Fatalf("alice2 want QUEUE got %+v err=%v", a2, err)
	}
	b1, err := s.ShadowDecide(ctx, m, bob, "bob", "p3", "event:x", "purchase", "purchase-per-event", 1, 1, ttl, "r3")
	if err != nil || b1.Would != "ALLOW" {
		t.Fatalf("bob must not line up behind alice: %+v err=%v", b1, err)
	}
	a3, err := s.ShadowDecide(ctx, m, alice, "alice", "p4", "event:x", "purchase", "purchase-per-event", 1, 1, ttl, "r4")
	if err != nil || a3.Would != "REJECT" {
		t.Fatalf("alice3 want REJECT got %+v err=%v", a3, err)
	}

	rep, err := s.DryRunReport(ctx, m, time.Hour, 10)
	if err != nil {
		t.Fatal(err)
	}
	if rep.WouldAllow < 2 || rep.WouldQueue < 1 || rep.WouldReject < 1 || rep.Affected < 1 {
		t.Fatalf("report %+v", rep)
	}
}

func TestControlsAudit(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	if err := s.EnsureMerchant(ctx, m, "test", "secret"); err != nil {
		t.Fatal(err)
	}
	c := ops.Default()
	c.Mode = ops.ModeDryRun
	c.UpdatedBy = "test"
	if _, err := s.PutControls(ctx, m, c); err != nil {
		t.Fatal(err)
	}
	got, found, err := s.GetControls(ctx, m)
	if err != nil || !found || got.Mode != ops.ModeDryRun {
		t.Fatalf("get %+v found=%v err=%v", got, found, err)
	}
	ev, err := s.LastAudit(ctx, m, "CONTROL_CHANGED")
	if err != nil || ev.Type != "CONTROL_CHANGED" {
		t.Fatalf("audit %+v err=%v", ev, err)
	}
}
