package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
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

func TestSimTixStaticAssetsServed(t *testing.T) {
	r := chi.NewRouter()
	r.Handle("/assets/*", staticHandler())
	srv := httptest.NewServer(r)
	defer srv.Close()
	for _, path := range []string{"/assets/simtix.css", "/assets/event-hero.jpg"} {
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
