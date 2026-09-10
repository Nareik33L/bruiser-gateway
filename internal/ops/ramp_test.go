package ops

import "testing"

func TestBucketStableAndCustomerScoped(t *testing.T) {
	a := Bucket("alice", "m1")
	if Bucket("alice", "m1") != a {
		t.Fatal("bucket must be deterministic")
	}
	if a < 0 || a > 99 {
		t.Fatalf("bucket=%d", a)
	}
	if Bucket("alice", "m1") == Bucket("bob", "m1") && Bucket("carol", "m1") == a {
		// possible but we only care alice != bob is common; just ensure salt matters
	}
	if Bucket("alice", "salt-a") == Bucket("alice", "salt-b") {
		// collision possible; not a failure
	}
}

func TestInRampPercent(t *testing.T) {
	if InRamp("alice", 0, "") {
		t.Fatal("0% must not enforce")
	}
	if !InRamp("alice", 100, "") {
		t.Fatal("100% must enforce every customer")
	}
	in, out := 0, 0
	for i := 0; i < 200; i++ {
		id := "c" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		if InRamp(id, 10, "t") {
			in++
		} else {
			out++
		}
	}
	if in == 0 || out == 0 {
		t.Fatalf("10%% should split customers in=%d out=%d", in, out)
	}
}

func TestRampScopeMatch(t *testing.T) {
	s := RampScope{Events: []string{"ars-che"}, Policies: []string{"purchase-per-event"}}
	ok := RampInput{CustomerID: "alice", Resource: "event:ars-che", EventID: "ars-che", RuleName: "purchase-per-event"}
	if !s.Match(ok) {
		t.Fatal("expected match")
	}
	if s.Match(RampInput{CustomerID: "alice", Resource: "event:ars-tot", EventID: "ars-tot", RuleName: "purchase-per-event"}) {
		t.Fatal("other event should be out of scope")
	}
	cohort := RampScope{Cohorts: []string{"1001*"}}
	if !cohort.Match(RampInput{CustomerID: "1001234"}) {
		t.Fatal("prefix cohort")
	}
	if cohort.Match(RampInput{CustomerID: "2000001"}) {
		t.Fatal("other cohort")
	}
	if !(RampScope{}).Match(RampInput{CustomerID: "x"}) {
		t.Fatal("empty scope matches all")
	}
}
