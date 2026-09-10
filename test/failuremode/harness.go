// Package failuremode is a lab-only harness for crash, partition, and
// concurrency tests. Nothing in cmd/ imports it; it is not in the shipped binary.
package failuremode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/config"
	"github.com/Nareik33L/bruiser-gateway/internal/lease"
	"github.com/Nareik33L/bruiser-gateway/internal/resource"
	"github.com/Nareik33L/bruiser-gateway/internal/testlab"
	"github.com/jackc/pgx/v5"
)

// Decision is the product answer each scenario must assert.
type Decision string

const (
	Deny  Decision = "deny"
	Allow Decision = "allow"
)

// CosmeticVariants are the nine RC1 retest strings that used to mint nine
// ACTIVE domains. They must fold to one identifier.
func CosmeticVariants(base string) []string {
	return resource.CosmeticVariants(base)
}

func Folded(raw string) string {
	got, err := resource.Canonical(raw)
	if err != nil {
		return ""
	}
	return got
}

type Harness struct {
	Lab testlab.Lab
}

func Start(t testing.TB) Harness {
	t.Helper()
	return Harness{Lab: testlab.Start(t, testlab.ArsenalProfile(t), func(c *config.Config) {
		c.RateSessions, c.RateAcquire, c.RateAuthorize = 1e6, 1e6, 1e6
		c.RateRenew, c.RateRelease = 1e6, 1e6
		c.RateMerchant, c.RateCustomer, c.RatePrincipal, c.RateIP = 1e6, 1e6, 1e6, 0
		c.MaxInFlight = 64
	})}
}

func (h Harness) Session(t testing.TB, customer, principal string) string {
	t.Helper()
	assertion, err := auth.IssueDevAssertion(h.Lab.Cfg.DevHMACSecret, customer, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"principal":{"type":"agent","id":%q}}`, principal)
	req, _ := http.NewRequest(http.MethodPost, h.Lab.Server.URL+"/v1/sessions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+assertion)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Token string `json:"session_token"`
	}
	if resp.StatusCode != http.StatusCreated || json.Unmarshal(raw, &out) != nil || out.Token == "" {
		t.Fatalf("session %d %s", resp.StatusCode, raw)
	}
	return out.Token
}

type AcquireResult struct {
	Status int
	ID     string
	Token  string
	Fence  int64
	Body   string
}

func (r AcquireResult) Granted() bool {
	return r.Status == http.StatusCreated || r.Status == http.StatusOK
}

func (r AcquireResult) FailOpen() bool {
	return r.Status == http.StatusOK && strings.Contains(r.Body, "fail-open")
}

func (h Harness) Acquire(t testing.TB, tok, resource, action string, extra http.Header) AcquireResult {
	t.Helper()
	if action == "" {
		action = "hold"
	}
	payload := fmt.Sprintf(`{"resource":%q,"action":%q}`, resource, action)
	req, _ := http.NewRequest(http.MethodPost, h.Lab.Server.URL+"/v1/executions/acquire", bytes.NewBufferString(payload))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	for k, vs := range extra {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return AcquireResult{Status: 0, Body: err.Error()}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := AcquireResult{Status: resp.StatusCode, Body: string(raw)}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if v, ok := m["execution_id"].(string); ok {
		out.ID = v
	}
	if v, ok := m["execution_token"].(string); ok {
		out.Token = v
	}
	if v, ok := m["fence"].(float64); ok {
		out.Fence = int64(v)
	}
	return out
}

func (h Harness) PutPolicy(t testing.TB, yamlText string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, h.Lab.Admin.URL+"/v1/policy", strings.NewReader(yamlText))
	req.Header.Set("Content-Type", "application/yaml")
	req.Header.Set("X-Bruiser-Admin-Secret", h.Lab.Cfg.AdminSecret)
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

func (h Harness) PutControls(t testing.TB, body string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, h.Lab.Admin.URL+"/v1/admin/controls", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Bruiser-Admin-Secret", h.Lab.Cfg.AdminSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put controls %d %s", resp.StatusCode, b)
	}
}

func (h Harness) ActiveCount(t testing.TB, customer string) int {
	t.Helper()
	execs, err := h.Lab.Store.ListActive(context.Background(), h.Lab.Cfg.MerchantID, customer, 100)
	if err != nil {
		t.Fatal(err)
	}
	return len(execs)
}

func (h Harness) Get(t testing.TB, id string) (lease.Execution, error) {
	t.Helper()
	return h.Lab.Store.Get(context.Background(), h.Lab.Cfg.MerchantID, id)
}

func (h Harness) ExpireDue(t testing.TB) int {
	t.Helper()
	n, err := h.Lab.Store.ExpireDue(context.Background(), 200)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// LockDomain holds the scarcity-domain row from a connection outside the
// lab pool so in-flight Acquire calls block until Unlock or the pool dies.
func (h Harness) LockDomain(t testing.TB, domainKey string) (unlock func()) {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, h.Lab.Cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		conn.Close(ctx)
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `select fence from domains where merchant_id=$1 and domain_key=$2 for update`, h.Lab.Cfg.MerchantID, domainKey); err != nil {
		_ = tx.Rollback(ctx)
		conn.Close(ctx)
		t.Fatal(err)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			_ = tx.Rollback(ctx)
			_ = conn.Close(ctx)
		})
	}
}

func PolicyMaxActive(k int) string {
	return fmt.Sprintf(`version: 1
defaults:
  lease_ttl: 60s
  max_lifetime: 15m
domains:
  - name: purchase-per-event
    match: { action: [purchase, hold] }
    scope: [customer, resource]
    max_active: %d
    precedence: [browser, agent]
  - name: discovery
    match: { action: [search] }
    control: none
fallback: deny
`, k)
}

// RestartControlPlane closes the live HTTP servers and starts a new process
// against the same merchant and database. Lease state must survive in Postgres.
func RestartControlPlane(t testing.TB, h Harness) Harness {
	t.Helper()
	h.Lab.API.Close()
	h.Lab.Server.Close()
	h.Lab.Admin.Close()
	cfg := h.Lab.Cfg
	next := testlab.Start(t, testlab.ArsenalProfile(t), func(c *config.Config) {
		*c = cfg
		c.RateSessions, c.RateAcquire, c.RateAuthorize = 1e6, 1e6, 1e6
		c.RateRenew, c.RateRelease = 1e6, 1e6
		c.RateMerchant, c.RateCustomer, c.RatePrincipal, c.RateIP = 1e6, 1e6, 1e6, 0
	})
	return Harness{Lab: next}
}

func MustParseOutcome(t testing.TB, got AcquireResult, want Decision, why string) {
	t.Helper()
	switch want {
	case Deny:
		if got.FailOpen() {
			t.Fatalf("%s: fail-open ALLOW, want deny (%d %s)", why, got.Status, got.Body)
		}
		if got.Status == http.StatusCreated {
			t.Fatalf("%s: granted, want deny (%s)", why, got.Body)
		}
	case Allow:
		if !got.Granted() || got.FailOpen() {
			t.Fatalf("%s: want allow grant, got %d %s", why, got.Status, got.Body)
		}
	default:
		t.Fatalf("unknown decision %q", want)
	}
}

func Hit(t testing.TB, h http.Handler, token string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/holds", nil)
	if token != "" {
		req.Header.Set("X-Bruiser-Execution", token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}
