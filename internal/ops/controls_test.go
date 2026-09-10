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
	if c.PassThrough() || c.EffectivePercent() != 100 {
		t.Fatal("defaults are 100% enforce")
	}
	c.Enforcement = false
	if !c.PassThrough() || c.EffectivePercent() != 0 {
		t.Fatal("enforcement off is 0% active (dry-run behaviour)")
	}
	c = FromEnv("dry-run", true, true, -1)
	if !c.DryRun() || c.EffectivePercent() != 0 {
		t.Fatalf("mode=%s percent=%d", c.Mode, c.EffectivePercent())
	}
}

func TestShouldEnforceDeterministic(t *testing.T) {
	c := Default()
	c.EnforcePercent = 10
	in := RampInput{CustomerID: "alice", Resource: "event:x"}
	first := c.ShouldEnforce(in)
	for i := 0; i < 20; i++ {
		if c.ShouldEnforce(in) != first {
			t.Fatal("same customer must not flip")
		}
	}
	c.EnforcePercent = 0
	if c.ShouldEnforce(in) {
		t.Fatal("0% never enforces")
	}
	c.EnforcePercent = 100
	c.Scope = RampScope{Events: []string{"ars-che"}}
	if c.ShouldEnforce(RampInput{CustomerID: "alice", EventID: "ars-tot", Resource: "event:ars-tot"}) {
		t.Fatal("out of scope must observe only")
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
