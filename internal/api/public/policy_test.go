package publicapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/testlab"
)

const policyMax2 = `version: 1
defaults:
  lease_ttl: 60s
  max_lifetime: 15m
domains:
  - name: purchase-per-event
    match: { action: [purchase, hold] }
    scope: [customer, resource]
    max_active: 2
    precedence: [browser, agent]
  - name: discovery
    match: { action: [search] }
    control: none
fallback: deny
`

const policyHousehold = `version: 1
defaults:
  lease_ttl: 45s
  max_lifetime: 10m
domains:
  - name: household-cap
    match: { action: [purchase, hold] }
    scope: [anchor:household_id, resource]
    max_active: 1
    on_missing_anchor: deny
    precedence: [browser, agent]
  - name: discovery
    match: { action: [search] }
    control: none
fallback: deny
`

func putPolicy(t *testing.T, url, secret, yaml string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, url+"/v1/policy", strings.NewReader(yaml))
	req.Header.Set("Content-Type", "application/yaml")
	req.Header.Set("X-Bruiser-Admin-Secret", secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put policy %d %s", resp.StatusCode, b)
	}
}

func TestLiveMaxActiveChange(t *testing.T) {
	srv, cfg := startServer(t)
	a1 := session(t, srv, cfg, "alice", "agent-1")
	a2 := session(t, srv, cfg, "alice", "agent-2")
	first := acquireJSON(t, srv, a1)
	if first.StatusCode != http.StatusCreated {
		t.Fatalf("first %d %s", first.StatusCode, first.Raw)
	}
	second := acquireJSON(t, srv, a2)
	if second.StatusCode != http.StatusConflict {
		t.Fatalf("second want 409 got %d %s", second.StatusCode, second.Raw)
	}
	putPolicy(t, srv.URL, cfg.EdgeSecret, policyMax2)
	again := acquireJSON(t, srv, a2)
	if again.StatusCode != http.StatusCreated {
		t.Fatalf("after max_active=2 want 201 got %d %s", again.StatusCode, again.Raw)
	}
	if again.ID == first.ID {
		t.Fatal("second grant should be a new execution")
	}
}

func TestHouseholdCapBlocksSecondAccount(t *testing.T) {
	srv, cfg := startServer(t)
	putPolicy(t, srv.URL, cfg.EdgeSecret, policyHousehold)
	alice := sessionAnchored(t, srv, cfg, "alice", "agent", "bot-a", map[string]string{"household_id": "hh-9"})
	bob := sessionAnchored(t, srv, cfg, "bob", "agent", "bot-b", map[string]string{"household_id": "hh-9"})
	missing := session(t, srv, cfg, "carol", "bot-c")

	g := acquireJSON(t, srv, alice)
	if g.StatusCode != http.StatusCreated {
		t.Fatalf("alice %d %s", g.StatusCode, g.Raw)
	}
	busy := acquireJSON(t, srv, bob)
	if busy.StatusCode != http.StatusConflict {
		t.Fatalf("bob want 409 got %d %s", busy.StatusCode, busy.Raw)
	}
	var body map[string]any
	_ = json.Unmarshal([]byte(busy.Raw), &body)
	if body["rule_name"] != "household-cap" {
		t.Fatalf("rule_name=%v %s", body["rule_name"], busy.Raw)
	}

	denied := acquireJSON(t, srv, missing)
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("missing anchor want 403 got %d %s", denied.StatusCode, denied.Raw)
	}
	_ = json.Unmarshal([]byte(denied.Raw), &body)
	if body["reason"] != "missing_anchor" {
		t.Fatalf("reason=%v", body["reason"])
	}
}

func TestAuthorizeHouseholdDeny(t *testing.T) {
	_, srv, cfg, _ := testlab.GatewayAPI(t, testlab.ArsenalProfile(t))
	putPolicy(t, srv.URL, cfg.EdgeSecret, policyHousehold)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/authorize", strings.NewReader(`{"method":"POST","path":"/api/events/ars-che/holds"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Bruiser-Edge-Secret", cfg.EdgeSecret)
	// box office cookie has membership_no only, no household_id
	cookie, err := auth.IssueBoxOfficeSession(cfg.DevHMACSecret, "1001234", 0)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", "boxoffice_session="+cookie)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403 got %d %s", resp.StatusCode, b)
	}
}
