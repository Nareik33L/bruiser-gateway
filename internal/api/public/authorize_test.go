package publicapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/testlab"
)

func TestAuthorizeSearchUncontrolled(t *testing.T) {
	srv, cfg := testlab.Gateway(t, testlab.ArsenalProfile(t))
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/authorize", bytes.NewBufferString(`{"method":"GET","path":"/api/events"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Bruiser-Edge-Secret", cfg.EdgeSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("search %d %s", resp.StatusCode, b)
	}
}

func TestAuthorizeUnawareBusyAndAlreadyHeld(t *testing.T) {
	srv, cfg := testlab.Gateway(t, testlab.ArsenalProfile(t))
	cookie1, err := auth.IssueBoxOfficeSession(cfg.DevHMACSecret, "1001234", 0)
	if err != nil {
		t.Fatal(err)
	}
	cookie2, err := auth.IssueBoxOfficeSession(cfg.DevHMACSecret, "1001234", 0)
	if err != nil {
		t.Fatal(err)
	}

	authorize := func(cookie string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/authorize", bytes.NewBufferString(`{"method":"POST","path":"/api/events/ars-che/holds"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Bruiser-Edge-Secret", cfg.EdgeSecret)
		req.Header.Set("Cookie", "boxoffice_session="+cookie)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		b, _ := io.ReadAll(resp.Body)
		_ = json.Unmarshal(b, &out)
		return resp.StatusCode, out
	}

	code, body := authorize(cookie1)
	if code != http.StatusOK || body["status"] != "ALLOW" {
		t.Fatalf("first hold want ALLOW got %d %v", code, body)
	}
	code, body = authorize(cookie1)
	if code != http.StatusOK || body["status"] != "ALLOW" {
		t.Fatalf("same cookie want ALREADY_HELD/ALLOW got %d %v", code, body)
	}
	code, body = authorize(cookie2)
	if code != http.StatusConflict || body["status"] != "BUSY" {
		t.Fatalf("second login want BUSY got %d %v", code, body)
	}
}
