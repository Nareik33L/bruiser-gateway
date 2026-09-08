package clock

import (
	"testing"
	"time"
)

func TestFrozenAdvance(t *testing.T) {
	start := time.Date(2026, 10, 4, 10, 2, 31, 0, time.UTC)
	f := NewFrozen(start)
	if !f.Now().Equal(start) {
		t.Fatalf("now = %s, want %s", f.Now(), start)
	}
	f.Advance(60 * time.Second)
	want := start.Add(60 * time.Second)
	if !f.Now().Equal(want) {
		t.Fatalf("now = %s, want %s", f.Now(), want)
	}
}
