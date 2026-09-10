package publicapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Nareik33L/bruiser-gateway/internal/config"
	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
	"github.com/Nareik33L/bruiser-gateway/internal/policy"
	"github.com/Nareik33L/bruiser-gateway/internal/testlab"
)

func TestAdminPaths404OnPublicListener(t *testing.T) {
	lab := testlab.Start(t, testlab.ArsenalProfile(t), nil)
	paths := []string{"/v1/policy", "/v1/admin/status", "/v1/admin/audit", "/admin", "/demo"}
	for _, path := range paths {
		req, _ := http.NewRequest(http.MethodGet, lab.Server.URL+path, nil)
		req.Header.Set("X-Bruiser-Admin-Secret", lab.Cfg.AdminSecret)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("public %s want 404 got %d", path, resp.StatusCode)
		}
	}
	req, _ := http.NewRequest(http.MethodGet, lab.Admin.URL+"/v1/admin/status", nil)
	req.Header.Set("X-Bruiser-Operator-Secret", lab.Cfg.OperatorSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("admin listener status %d %s", resp.StatusCode, b)
	}
	req, _ = http.NewRequest(http.MethodPost, lab.Server.URL+"/v1/introspect", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		t.Fatal("introspect must stay on the public listener")
	}
}

func TestOperatorCannotPutPolicyAdminCan(t *testing.T) {
	lab := testlab.Start(t, testlab.ArsenalProfile(t), nil)
	raw, err := yamlPolicy(lab.Cfg.MerchantID)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPut, lab.Admin.URL+"/v1/policy", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/yaml")
	req.Header.Set("X-Bruiser-Operator-Secret", lab.Cfg.OperatorSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("operator PUT policy want 403 got %d %s", resp.StatusCode, b)
	}
	req, _ = http.NewRequest(http.MethodPut, lab.Admin.URL+"/v1/policy", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/yaml")
	req.Header.Set("X-Bruiser-Admin-Secret", lab.Cfg.AdminSecret)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin PUT policy want 200 got %d %s", resp.StatusCode, b)
	}
}

func TestPolicyWriteEmitsOneAuditRecord(t *testing.T) {
	lab := testlab.Start(t, testlab.ArsenalProfile(t), nil)
	before := policy.HashYAML(nil)
	if row, err := lab.Store.ActivePolicy(t.Context(), lab.Cfg.MerchantID); err == nil {
		before = policy.HashYAML([]byte(row.YAML))
	}
	raw, err := yamlPolicy(lab.Cfg.MerchantID)
	if err != nil {
		t.Fatal(err)
	}
	after := policy.HashYAML(raw)
	req, _ := http.NewRequest(http.MethodPut, lab.Admin.URL+"/v1/policy", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/yaml")
	req.Header.Set("X-Bruiser-Admin-Secret", lab.Cfg.AdminSecret)
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put %d %s", resp.StatusCode, b)
	}
	req, _ = http.NewRequest(http.MethodGet, lab.Admin.URL+"/v1/admin/audit?type=POLICY_CHANGED&limit=20", nil)
	req.Header.Set("X-Bruiser-Operator-Secret", lab.Cfg.OperatorSecret)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	rawBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("audit %d %s", resp.StatusCode, rawBody)
	}
	var out struct {
		Events []struct {
			Type          string         `json:"type"`
			PrincipalID   string         `json:"principal_id"`
			PrincipalType string         `json:"principal_type"`
			Attrs         map[string]any `json:"attrs"`
		} `json:"events"`
	}
	if err := json.Unmarshal(rawBody, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Events) != 1 {
		t.Fatalf("want exactly one POLICY_CHANGED, got %d %s", len(out.Events), rawBody)
	}
	ev := out.Events[0]
	if ev.Type != "POLICY_CHANGED" || ev.PrincipalID != "admin-secret" || ev.PrincipalType != "admin" {
		t.Fatalf("event %+v", ev)
	}
	if ev.Attrs["before_hash"] != before || ev.Attrs["after_hash"] != after {
		t.Fatalf("hashes before=%v after=%v want %s %s", ev.Attrs["before_hash"], ev.Attrs["after_hash"], before, after)
	}
	if ev.Attrs["ip"] != "203.0.113.9" {
		t.Fatalf("ip=%v", ev.Attrs["ip"])
	}
}

func TestAdminAccessTokenRoles(t *testing.T) {
	lab := testlab.Start(t, testlab.ArsenalProfile(t), nil)
	req, _ := http.NewRequest(http.MethodPost, lab.Admin.URL+"/v1/admin/token", bytes.NewBufferString(`{"role":"operator","ttl_seconds":60}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Bruiser-Admin-Secret", lab.Cfg.AdminSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var tok struct {
		Token string `json:"token"`
		Role  string `json:"role"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&tok)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || tok.Token == "" || tok.Role != "operator" {
		t.Fatalf("mint %d %+v", resp.StatusCode, tok)
	}
	raw, err := yamlPolicy(lab.Cfg.MerchantID)
	if err != nil {
		t.Fatal(err)
	}
	req, _ = http.NewRequest(http.MethodPut, lab.Admin.URL+"/v1/policy", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+tok.Token)
	req.Header.Set("Content-Type", "application/yaml")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("operator token PUT want 403 got %d", resp.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodGet, lab.Admin.URL+"/v1/policy", nil)
	req.Header.Set("Authorization", "Bearer "+tok.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("operator token GET policy %d", resp.StatusCode)
	}
}

func TestProductionRejectsUnmatchedAllowWithoutUnsafeFlag(t *testing.T) {
	p, err := merchant.Parse([]byte(`
merchant_id: example
unmatched: allow
identity:
  extractor: oidc
  jwks_url: https://idp.example/.well-known/jwks.json
  issuer: https://idp.example
  audience: bruiser
routes:
  - match: { method: POST, path: "/api/holds" }
    resource: "sku:one"
    action: hold
`))
	if err != nil {
		t.Fatal(err)
	}
	err = merchant.ValidateProductionPolicy(p, false)
	if err == nil || !strings.Contains(err.Error(), "unmatched: allow") || !strings.Contains(err.Error(), "BRUISER_ALLOW_UNSAFE_MODES") {
		t.Fatalf("want unmatched allow rejection, got %v", err)
	}
	if err := merchant.ValidateProductionPolicy(p, true); err != nil {
		t.Fatal(err)
	}
	p.Unmatched = "deny"
	if err := merchant.ValidateProductionPolicy(p, false); err != nil {
		t.Fatal(err)
	}
}

func TestProductionBootRefusesLabAdminSecret(t *testing.T) {
	c := config.Load()
	c.Environment = "production"
	c.AdminSecret = "admin-secret-dev"
	c.OperatorSecret = "prod-operator-unique"
	c.EdgeSecret = "prod-edge-unique"
	c.OriginSecret = "prod-origin-unique"
	c.DevHMACSecret = "prod-hmac-unique"
	c.AdminAddr = "127.0.0.1:8082"
	c.JWKSURL = "https://idp.example/.well-known/jwks.json"
	c.Issuer = "https://idp.example"
	c.Audience = "bruiser"
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "BRUISER_ADMIN_SECRET") {
		t.Fatalf("lab admin secret in production: %v", err)
	}
}

func yamlPolicy(merchant string) ([]byte, error) {
	_ = merchant
	return []byte(`version: 1
defaults:
  lease_ttl: 60s
  max_lifetime: 15m
domains:
  - name: purchase-per-event
    match: { action: [purchase, hold] }
    scope: [customer, resource]
    max_active: 1
    precedence: [browser, agent]
  - name: discovery
    match: { action: [search] }
    control: none
fallback: deny
`), nil
}
