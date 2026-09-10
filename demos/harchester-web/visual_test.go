package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
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

func TestClubStaticAssetsServed(t *testing.T) {
	r := chi.NewRouter()
	r.Handle("/assets/*", staticHandler())
	srv := httptest.NewServer(r)
	defer srv.Close()
	for _, path := range []string{"/assets/club.css", "/assets/hero-stadium.jpg"} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("%s → %d", path, resp.StatusCode)
		}
	}
}
