package ops

import "testing"

func TestEffectiveWaiters(t *testing.T) {
	c := Default()
	if c.EffectiveWaiters(3) != 3 {
		t.Fatalf("policy waiters: %d", c.EffectiveWaiters(3))
	}
	c.QueueEnabled = false
	if c.EffectiveWaiters(3) != 0 {
		t.Fatalf("queue off should zero waiters")
	}
	c.QueueEnabled = true
	c.MaxWaiters = 2
	if c.EffectiveWaiters(9) != 2 {
		t.Fatalf("override: %d", c.EffectiveWaiters(9))
	}
}

func TestPassThroughAndDryRun(t *testing.T) {
	c := Default()
	if c.PassThrough() || c.DryRun() {
		t.Fatal("defaults enforce")
	}
	c.Enforcement = false
	if !c.PassThrough() {
		t.Fatal("enforcement off is pass-through")
	}
	c = FromEnv("dry-run", true, true)
	if !c.DryRun() || c.Mode != ModeDryRun {
		t.Fatalf("mode=%s", c.Mode)
	}
}

func TestActionDisabled(t *testing.T) {
	c := Default()
	c.DisabledActions = []string{"purchase", "hold"}
	if !c.ActionDisabled("PURCHASE") || !c.ActionDisabled("hold") {
		t.Fatal("disabled actions")
	}
	if c.ActionDisabled("search") {
		t.Fatal("search should stay enabled")
	}
}
