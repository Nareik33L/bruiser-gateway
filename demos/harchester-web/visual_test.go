package main

import (
	"strings"
	"testing"
)

func TestClubVisualTokens(t *testing.T) {
	b, err := staticFS.ReadFile("static/club.css")
	if err != nil {
		t.Fatal(err)
	}
	css := string(b)
	for _, tok := range []string{"#4C1D7A", "#0B0B0F", "#F3F1F5", "#E85D04", "system-ui"} {
		if !strings.Contains(css, tok) {
			t.Fatalf("club.css missing %s", tok)
		}
	}
	for _, file := range []string{
		"static/hero-stadium.jpg",
		"static/news-matchday.jpg",
		"static/news-player.jpg",
		"static/match-banner.jpg",
	} {
		if _, err := staticFS.ReadFile(file); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
	}
}
