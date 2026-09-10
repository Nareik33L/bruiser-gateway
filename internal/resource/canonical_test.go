package resource

import (
	"testing"
	"unicode/utf8"
)

func TestCanonicalCupfinalVariants(t *testing.T) {
	want := "ticket:cupfinal"
	for _, in := range CupfinalCorpus() {
		got, err := Canonical(in)
		if err != nil || got != want {
			t.Fatalf("%q → %q %v want %s", in, got, err, want)
		}
	}
}

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
		"event/ars-che",
		"event:ars-che.",
		"event:ars-che-",
		"event:ars‐che", // U+2010
		"event:ars–che", // U+2013
		"event: ars-che",
	} {
		got, err := Canonical(in)
		if err != nil || got != want {
			t.Fatalf("%q → %q %v want %s", in, got, err, want)
		}
	}
}

func TestCanonicalRejectsMalformed(t *testing.T) {
	for _, in := range []string{
		"",
		"   ",
		"event:../x",
		"event:?q=1",
		"event:#frag",
		"///",
		":",
		"ars-che",
		"event:ars:che",
		"event:ars_che",
		"event:ars.che",
		"event:\\x",
	} {
		if got, err := Canonical(in); err == nil {
			t.Fatalf("expected malformed %q got %q", in, got)
		}
	}
}

func TestCatalogueResolvesFoldedID(t *testing.T) {
	cat := NewCatalogue([]string{"ticket:cupfinal", "event:ars-che"})
	got, err := cat.Canonical("ticket:cupfinal.")
	if err != nil || got != "ticket:cupfinal" {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := cat.Canonical("ticket:other"); err != ErrUnknownResource {
		t.Fatalf("unknown: %v", err)
	}
	empty := NewCatalogue(nil)
	got, err = empty.Canonical("event:final")
	if err != nil || got != "event:final" {
		t.Fatalf("empty catalogue %q %v", got, err)
	}
}

func TestCanonicalIdempotent(t *testing.T) {
	for _, in := range append(CupfinalCorpus(), "EVENT:ARS-CHE/", "event:ars–che") {
		got, err := Canonical(in)
		if err != nil {
			t.Fatalf("%q %v", in, err)
		}
		again, err := Canonical(got)
		if err != nil || again != got {
			t.Fatalf("not idempotent %q → %q → %q %v", in, got, again, err)
		}
	}
}

func FuzzCanonical(f *testing.F) {
	for _, in := range CupfinalCorpus() {
		f.Add(in)
	}
	for _, in := range []string{
		"event:ars-che",
		"EVENT:ARS-CHE/",
		"event:ars–che",
		"sku:abc",
		"",
		"..",
		"event:ars:che",
	} {
		f.Add(in)
	}
	wantCup, _ := Canonical("ticket:cupfinal")
	f.Fuzz(func(t *testing.T, in string) {
		if !utf8.ValidString(in) {
			return
		}
		got, err := Canonical(in)
		if err != nil {
			return
		}
		again, err2 := Canonical(got)
		if err2 != nil || again != got {
			t.Fatalf("idempotency %q → %q → %q %v", in, got, again, err2)
		}
		for _, suf := range []string{".", "-", "_", "!", "@", " ", string(rune(0x2010)), string(rune(0x2013))} {
			folded, ferr := Canonical(got + suf)
			if ferr != nil || folded != got {
				t.Fatalf("trailing %q split %q vs %q (%v)", suf, got, folded, ferr)
			}
		}
		if wantCup != "" && got == wantCup {
			return
		}
	})
}
