package simtix

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOriginLockdownBlocksDirectHold(t *testing.T) {
	s := New(Config{HMACSecret: "dev-secret-change-me", OriginSecret: "lock", Seats: 4})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/api/events/ars-che/holds", "application/json", strings.NewReader(`{"seats":1}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403 got %d", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/events/ars-che/holds", strings.NewReader(`{"seats":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Bruiser-Origin-Secret", "lock")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("lockdown allow %d", resp.StatusCode)
	}
}

func TestSearchNeverRequiresOriginSecret(t *testing.T) {
	s := New(Config{HMACSecret: "dev-secret-change-me", OriginSecret: "lock", Seats: 4})
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("search %d", resp.StatusCode)
	}
}
