package lease

import (
	"testing"
	"time"
)

func TestIsActiveDerivedExpiry(t *testing.T) {
	now := time.Date(2026, 10, 4, 10, 3, 0, 0, time.UTC)
	e := Execution{
		State:     StateActive,
		ExpiresAt: now.Add(-time.Second),
	}
	if e.IsActive(now) {
		t.Fatal("expired ACTIVE row must not be treated as active")
	}
	e.ExpiresAt = now.Add(time.Second)
	if !e.IsActive(now) {
		t.Fatal("unexpired ACTIVE row must be active")
	}
	e.State = StateReleased
	if e.IsActive(now) {
		t.Fatal("RELEASED must not be active")
	}
}

func TestDomainKey(t *testing.T) {
	got := DomainKey("arsenal", "purchase-per-event", "cust_alice", "event:ars-che")
	want := "arsenal/purchase-per-event/customer=cust_alice/resource=event:ars-che"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
