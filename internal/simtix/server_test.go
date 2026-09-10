package simtix

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOriginLockdownBlocksDirectHold(t *testing.T) {
	signer := testSigner(t, "arsenal")
	s := New(Lab("dev-secret-change-me", "lock", "arsenal", signer.Public, "", 4))
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Post(srv.URL+"/api/events/ars-che/holds", "application/json", strings.NewReader(`{"seats":1}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusCreated {
		t.Fatal("bare hold must not allocate")
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/events/ars-che/holds", strings.NewReader(`{"seats":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Bruiser-Origin-Secret", "lock")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusCreated {
		t.Fatal("origin secret alone must not allocate")
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
