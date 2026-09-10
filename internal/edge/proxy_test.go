package edge

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnforceBlocksBusy(t *testing.T) {
	originHits := 0
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originHits++
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(origin.Close)
	bruiser := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"status":"BUSY"}`))
	}))
	t.Cleanup(bruiser.Close)

	p, err := New(Config{OriginURL: origin.URL, BruiserURL: bruiser.URL, Mode: ModeEnforce})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(p.Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/s/demo/checkout", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("want 409 got %d", resp.StatusCode)
	}
	if resp.Header.Get(HeaderDecision) != "BUSY" {
		t.Fatalf("decision %s", resp.Header.Get(HeaderDecision))
	}
	if resp.Header.Get(HeaderForwarded) != "0" {
		t.Fatalf("should not forward")
	}
	if originHits != 0 {
		t.Fatalf("origin hit %d", originHits)
	}
}

func TestDryRunForwardsBusy(t *testing.T) {
	var gotSecret string
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSecret = r.Header.Get("X-Bruiser-Origin-Secret")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"order":true}`))
	}))
	t.Cleanup(origin.Close)
	bruiser := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"status":"BUSY"}`))
	}))
	t.Cleanup(bruiser.Close)

	p, err := New(Config{
		OriginURL:    origin.URL,
		BruiserURL:   bruiser.URL,
		Mode:         ModeDryRun,
		OriginSecret: "lab-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(p.Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/s/demo/checkout", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("want origin 201 got %d %s", resp.StatusCode, b)
	}
	if resp.Header.Get(HeaderDecision) != "BUSY" {
		t.Fatalf("decision %s", resp.Header.Get(HeaderDecision))
	}
	if resp.Header.Get(HeaderForwarded) != "1" {
		t.Fatalf("dry-run must forward")
	}
	if resp.Header.Get(HeaderWouldBlock) != "1" {
		t.Fatalf("would-block header")
	}
	if gotSecret != "lab-secret" {
		t.Fatalf("origin secret %q", gotSecret)
	}
}

func TestOffSkipsAuthorize(t *testing.T) {
	authHits := 0
	originHits := 0
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		originHits++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"open":true}`))
	}))
	t.Cleanup(origin.Close)
	bruiser := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		authHits++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(bruiser.Close)

	p, err := New(Config{OriginURL: origin.URL, BruiserURL: bruiser.URL, Mode: ModeOff})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(p.Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/s/demo")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if authHits != 0 {
		t.Fatalf("authorize called")
	}
	if originHits != 1 {
		t.Fatalf("origin hits %d", originHits)
	}
}

func TestInProcessHandlers(t *testing.T) {
	origin := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/s/x/checkout" {
			t.Errorf("path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
	})
	bruiser := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ALLOW"})
	})
	p, err := New(Config{
		OriginHandler:  origin,
		BruiserHandler: bruiser,
		Mode:           ModeEnforce,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/s/x/checkout", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("code %d", rec.Code)
	}
	if rec.Header().Get(HeaderDecision) != "ALLOW" {
		t.Fatalf("decision %s", rec.Header().Get(HeaderDecision))
	}
}
