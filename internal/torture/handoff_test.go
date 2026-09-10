package torture

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/api/public"
	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/config"
	"github.com/Nareik33L/bruiser-gateway/internal/id"
	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
	pgstore "github.com/Nareik33L/bruiser-gateway/internal/store/postgres"
)

// TestHandoffRevokeInvariants exercises I2 (fence rises on successor) and
// I4 (renew after terminal state fails) across three gateway processes.
func TestHandoffRevokeInvariants(t *testing.T) {
	url := os.Getenv("BRUISER_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("BRUISER_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	if err := pgstore.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	store, err := pgstore.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)

	cfg := config.Load()
	cfg.DatabaseURL = url
	cfg.MerchantID = id.New("m")
	cfg.LeaseTTL = 30 * time.Second
	cfg.Environment = "lab"
	cfg.DevAssertions = true
	cfg.RateSessions, cfg.RateAcquire, cfg.RateMerchant = 1e6, 1e6, 1e6
	cfg.RateCustomer, cfg.RatePrincipal = 1e6, 1e6
	if err := store.EnsureMerchant(ctx, cfg.MerchantID, "test", cfg.DevHMACSecret); err != nil {
		t.Fatal(err)
	}
	key, err := store.EnsureSigningKey(ctx, cfg.MerchantID)
	if err != nil {
		t.Fatal(err)
	}
	signer := auth.Signer{KID: key.KID, MerchantID: cfg.MerchantID, Private: key.Private, Public: key.Public}

	srvs := make([]*httptest.Server, 3)
	for i := 0; i < 3; i++ {
		st, err := pgstore.Connect(ctx, url)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(st.Close)
		h := publicapi.New(cfg, st, signer, slog.New(slog.NewTextHandler(io.Discard, nil)), merchant.Empty(cfg.MerchantID))
		srvs[i] = httptest.NewServer(h)
		t.Cleanup(srvs[i].Close)
	}

	agentTok := mustSession(t, srvs[0], cfg, "alice", "agent-1")
	browserTok := sessionType(t, srvs[1], cfg, "alice", "browser", "tab")

	acq := post(t, srvs[0].URL+"/v1/executions/acquire", `{"resource":"event:final","action":"purchase"}`, agentTok)
	if acq.Status != 201 {
		t.Fatalf("acquire %d %s", acq.Status, acq.Raw)
	}

	handoff := post(t, srvs[1].URL+"/v1/executions/"+acq.ID+"/handoff", `{"mode":"preempt"}`, browserTok)
	if handoff.Status != 201 {
		t.Fatalf("handoff %d %s", handoff.Status, handoff.Raw)
	}
	if handoff.Fence <= acq.Fence {
		t.Fatalf("I2 fence %d → %d", acq.Fence, handoff.Fence)
	}

	renew := post(t, srvs[2].URL+"/v1/executions/"+acq.ID+"/renew", "", agentTok)
	if renew.Status != 410 {
		t.Fatalf("I4 agent renew want 410 got %d %s", renew.Status, renew.Raw)
	}

	var active int
	if err := store.Pool().QueryRow(ctx, `
		select count(*) from executions
		where merchant_id=$1 and state='ACTIVE' and expires_at>now()`,
		cfg.MerchantID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("ACTIVE=%d want 1 after handoff", active)
	}

	rev := post(t, srvs[0].URL+"/v1/executions/"+handoff.ID+"/revoke", `{"reason":"customer_stop"}`, browserTok)
	if rev.Status != 200 {
		t.Fatalf("revoke %d %s", rev.Status, rev.Raw)
	}
	z := post(t, srvs[1].URL+"/v1/executions/"+handoff.ID+"/renew", "", browserTok)
	if z.Status != 410 {
		t.Fatalf("I4 renew after revoke want 410 got %d %s", z.Status, z.Raw)
	}
	if err := store.Pool().QueryRow(ctx, `
		select count(*) from executions
		where merchant_id=$1 and state='ACTIVE' and expires_at>now()`,
		cfg.MerchantID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 0 {
		t.Fatalf("ACTIVE=%d after revoke", active)
	}
}

type httpBody struct {
	Status int
	Raw    string
	ID     string
	Fence  int64
}

func post(t *testing.T, url, body, bearer string) httpBody {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = bytes.NewBufferString(body)
	}
	req, _ := http.NewRequest(http.MethodPost, url, rdr)
	req.Header.Set("Authorization", "Bearer "+bearer)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := httpBody{Status: resp.StatusCode, Raw: string(raw)}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if v, ok := m["execution_id"].(string); ok {
		out.ID = v
	}
	if v, ok := m["fence"].(float64); ok {
		out.Fence = int64(v)
	}
	return out
}

func sessionType(t *testing.T, srv *httptest.Server, cfg config.Config, customer, ptype, principal string) string {
	t.Helper()
	assertion, err := auth.IssueDevAssertion(cfg.DevHMACSecret, customer, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"principal":{"type":%q,"id":%q}}`, ptype, principal)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/sessions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+assertion)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("session %d %s", resp.StatusCode, b)
	}
	var out struct {
		Token string `json:"session_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Token
}
