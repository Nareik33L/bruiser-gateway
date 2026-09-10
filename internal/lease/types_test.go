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
	want := "arsenal/purchase-per-event/customer=cust_alice/resource=event:ars-che"
	for _, res := range []string{"event:ars-che", "EVENT:ARS-CHE/", "event:ars-che.", "event:ars–che"} {
		got := DomainKey("arsenal", "purchase-per-event", "cust_alice", res)
		if got != want {
			t.Fatalf("%q got %q want %q", res, got, want)
		}
	}
}
