package main

import (
	"strings"
	"testing"
)

func TestSimTixVisualTokens(t *testing.T) {
	b, err := staticFS.ReadFile("static/simtix.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(b)
	for _, tok := range []string{"#0B1B3D", "#026CDF", "system-ui"} {
		if !strings.Contains(css, tok) {
			t.Fatalf("simtix.css missing %s", tok)
		}
	}
	if _, err := staticFS.ReadFile("static/event-hero.jpg"); err != nil {
		t.Fatal(err)
	}
}
