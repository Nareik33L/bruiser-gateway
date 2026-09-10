package policy

import (
	"strings"
	"testing"
)

func TestCheckProductionSafetyRejectsAllowUncontrolled(t *testing.T) {
	doc := DefaultDocument("m")
	doc.Fallback = FallbackAllowUncontrolled
	err := CheckProductionSafety(doc, false)
	if err == nil || !strings.Contains(err.Error(), "allow-uncontrolled") || !strings.Contains(err.Error(), "BRUISER_ALLOW_UNSAFE_MODES") {
		t.Fatalf("want fail-open rejection, got %v", err)
	}
	if err := CheckProductionSafety(doc, true); err != nil {
		t.Fatal(err)
	}
}

func TestCheckProductionSafetyAllowsDiscoveryNone(t *testing.T) {
	doc := DefaultDocument("m")
	if err := CheckProductionSafety(doc, false); err != nil {
		t.Fatalf("discovery control:none must not be treated as fail-open: %v", err)
	}
	if got := FailOpenReasons(doc); len(got) != 0 {
		t.Fatalf("reasons=%v", got)
	}
}

func TestHashYAMLStable(t *testing.T) {
	a := HashYAML([]byte("fallback: deny\n"))
	b := HashYAML([]byte("fallback: deny\n"))
	c := HashYAML([]byte("fallback: allow-uncontrolled\n"))
	if a == "" || a != b {
		t.Fatalf("hash %s %s", a, b)
	}
	if a == c {
		t.Fatal("different yaml must not collide")
	}
}
