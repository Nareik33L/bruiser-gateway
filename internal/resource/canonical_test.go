package resource

import "testing"

func TestCanonicalVariants(t *testing.T) {
	want := "event:ars-che"
	for _, in := range []string{
		"event:ars-che",
		"EVENT:ARS-CHE",
		" event:ars-che ",
		"event:ars-che/",
		"event:ars-che///",
		"Event:Ars-Che",
		"event%3Aars-che",
	} {
		got, err := Canonical(in)
		if err != nil || got != want {
			t.Fatalf("%q → %q %v want %s", in, got, err, want)
		}
	}
}

func TestCanonicalRejectsLookalikes(t *testing.T) {
	// Fable attack: punctuation / Unicode variants must not fold onto
	// event:ars-che. They are rejected, not remapped.
	for _, in := range []string{
		"event:ars-che.",
		"event:ars-che-",
		"event:ars-che_",
		"event:ars‐che", // U+2011 non-breaking hyphen
		"event:ars–che", // U+2013 en-dash
		"event:ars:che",
		"event:/ars-che",
		"event://ars-che",
		"event/ars-che",
		"event/ars-che/",
		"event:ars-che:",
		"event: ars-che",
		"event:ars_che",
		"event:ars.che",
		"",
		"   ",
		"event:../x",
		"event:?q=1",
		"event:#frag",
		"///",
		":",
		"ars-che",
	} {
		if got, err := Canonical(in); err == nil {
			t.Fatalf("expected malformed %q got %q", in, got)
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
