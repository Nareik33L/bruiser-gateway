package resource

import "testing"

func TestCanonicalVariants(t *testing.T) {
	want := "event:ars-che"
	for _, in := range []string{
		"event:ars-che",
		"EVENT:ARS-CHE",
		" event:ars-che ",
		"event:ars-che/",
		"event:/ars-che",
		"event://ars-che",
		"event/ars-che",
		"event/ars-che/",
		"event:ars-che:",
		"Event:Ars-Che",
		"event%3Aars-che",
		"event: ars-che",
	} {
		got, err := Canonical(in)
		if err != nil || got != want {
			t.Fatalf("%q → %q %v want %s", in, got, err, want)
		}
	}
}

func TestCanonicalRejectsMalformed(t *testing.T) {
	for _, in := range []string{"", "   ", "event:../x", "event:?q=1", "event:#frag", "///", ":"} {
		if _, err := Canonical(in); err == nil {
			t.Fatalf("expected malformed %q", in)
		}
	}
}
