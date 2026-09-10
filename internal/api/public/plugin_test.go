package publicapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
	"github.com/Nareik33L/bruiser-gateway/internal/testlab"
)

const pluginYAML = `
merchant_id: plugin
unmatched: allow
identity:
  extractor: auto
  cookie: session
  header: X-Customer-Id
  principal_header: X-Principal-Id
  subject_claim: sub
routes:
  - match: { method: GET, path: "/catalog" }
    action: search
  - match: { method: POST, path: "/api/holds" }
    resource_from: event_id
    resource_prefix: "sku:"
    action: hold
policy:
  version: 1
  defaults: { lease_ttl: 60s, max_lifetime: 15m }
  domains:
    - name: one
      match: { action: [hold] }
      scope: [customer, resource]
      max_active: 1
  fallback: deny
`

func pluginProfile(t *testing.T) merchant.Profile {
	t.Helper()
	p, err := merchant.Parse([]byte(pluginYAML))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUnmatchedRoutesPassThrough(t *testing.T) {
	srv, cfg := testlab.Gateway(t, pluginProfile(t))
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/authorize", bytes.NewBufferString(`{"method":"GET","path":"/css/app.css"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Bruiser-Edge-Secret", cfg.EdgeSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unmatched want 200 got %d %s", resp.StatusCode, b)
	}
	var body map[string]any
	_ = json.Unmarshal(b, &body)
	if body["action"] != "unmatched" {
		t.Fatalf("body=%v", body)
	}
}

func TestHeaderExtractorTwoAgentsBusy(t *testing.T) {
	srv, cfg := testlab.Gateway(t, pluginProfile(t))
	authz := func(principal string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/authorize",
			bytes.NewBufferString(`{"method":"POST","path":"/api/holds","event_id":"sku-1"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Bruiser-Edge-Secret", cfg.EdgeSecret)
		req.Header.Set("X-Customer-Id", "alice")
		req.Header.Set("X-Principal-Id", principal)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		raw, _ := io.ReadAll(resp.Body)
		_ = json.Unmarshal(raw, &out)
		return resp.StatusCode, out
	}
	code, body := authz("agent-a")
	if code != http.StatusOK || body["status"] != "ALLOW" {
		t.Fatalf("first %d %v", code, body)
	}
	code, body = authz("agent-b")
	if code != http.StatusConflict || body["status"] != "BUSY" {
		t.Fatalf("second want BUSY got %d %v", code, body)
	}
}

func TestBearerJWTExtractor(t *testing.T) {
	srv, cfg := testlab.Gateway(t, pluginProfile(t))
	tok, err := auth.IssueDevAssertion(cfg.DevHMACSecret, "bob", time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/authorize",
		bytes.NewBufferString(`{"method":"POST","path":"/api/holds","event_id":"sku-9"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Bruiser-Edge-Secret", cfg.EdgeSecret)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("bearer %d %s", resp.StatusCode, b)
	}
}

func TestExampleProfileLoads(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	var p merchant.Profile
	found := false
	for i := 0; i < 8; i++ {
		path := filepath.Join(dir, "configs", "example.yaml")
		if _, err := os.Stat(path); err == nil {
			p, err = merchant.LoadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			found = true
			break
		}
		dir = filepath.Dir(dir)
	}
	if !found {
		t.Fatal("configs/example.yaml not found")
	}
	if p.MerchantID != "example" || p.UnmatchedAllow() {
		t.Fatalf("example profile should ship unmatched deny: %+v", p)
	}
}
