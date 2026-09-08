package publicapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"log/slog"

	"github.com/Nareik33L/bruiser-gateway/internal/api/public"
	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/config"
	"github.com/Nareik33L/bruiser-gateway/internal/id"
	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
	pgstore "github.com/Nareik33L/bruiser-gateway/internal/store/postgres"
)

func startServer(t *testing.T) (*httptest.Server, config.Config) {
	t.Helper()
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
	cfg.LeaseTTL = 15 * time.Second
	cfg.EdgeSecret = "edge-secret-dev"
	cfg.AdminSecret = "admin-secret-dev"
	cfg.DevAssertions = true
	cfg.RateSessions, cfg.RateAcquire, cfg.RateAuthorize = 1e6, 1e6, 1e6
	cfg.RateMerchant, cfg.RateCustomer, cfg.RatePrincipal = 1e6, 1e6, 1e6
	if err := store.EnsureMerchant(ctx, cfg.MerchantID, "test", cfg.DevHMACSecret); err != nil {
		t.Fatal(err)
	}
	key, err := store.EnsureSigningKey(ctx, cfg.MerchantID)
	if err != nil {
		t.Fatal(err)
	}
	signer := auth.Signer{KID: key.KID, MerchantID: cfg.MerchantID, Private: key.Private, Public: key.Public}
	h := publicapi.New(cfg, store, signer, slog.New(slog.NewTextHandler(io.Discard, nil)), merchant.Empty(cfg.MerchantID))
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, cfg
}

func session(t *testing.T, srv *httptest.Server, cfg config.Config, customer, principal string) string {
	return sessionAs(t, srv, cfg, customer, "agent", principal)
}

func sessionAs(t *testing.T, srv *httptest.Server, cfg config.Config, customer, ptype, principal string) string {
	return sessionAnchored(t, srv, cfg, customer, ptype, principal, nil)
}

func sessionAnchored(t *testing.T, srv *httptest.Server, cfg config.Config, customer, ptype, principal string, anchors map[string]string) string {
	t.Helper()
	assertion, err := auth.IssueDevAssertion(cfg.DevHMACSecret, customer, time.Hour, anchors)
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
		t.Fatalf("session %d: %s", resp.StatusCode, b)
	}
	var out struct {
		Token string `json:"session_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Token
}

func TestHealthAndReady(t *testing.T) {
	srv, _ := startServer(t)
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("healthz %d", resp.StatusCode)
	}
	resp, err = http.Get(srv.URL + "/readyz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("readyz %d", resp.StatusCode)
	}
}

func TestAcquireBusyRenewRelease(t *testing.T) {
	srv, cfg := startServer(t)
	tok1 := session(t, srv, cfg, "alice", "agent-1")
	tok2 := session(t, srv, cfg, "alice", "agent-2")

	acquire := func(tok string) *http.Response {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/executions/acquire", bytes.NewBufferString(`{"resource":"event:ars-che","action":"purchase"}`))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	resp := acquire(tok1)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("acquire agent1 %d %s", resp.StatusCode, b)
	}
	var granted struct {
		ID    string `json:"execution_id"`
		Token string `json:"execution_token"`
		Fence int64  `json:"fence"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&granted); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if granted.Token == "" {
		t.Fatal("missing execution token")
	}

	resp = acquire(tok2)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("agent2 want 409 got %d %s", resp.StatusCode, body)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/executions/"+granted.ID+"/renew", nil)
	req.Header.Set("Authorization", "Bearer "+tok1)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("renew %d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/v1/executions/"+granted.ID+"/release", nil)
	req.Header.Set("Authorization", "Bearer "+tok1)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("release %d", resp.StatusCode)
	}

	resp = acquire(tok2)
	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("reacquire %d %s", resp.StatusCode, b)
	}
	resp.Body.Close()
}

func TestHeartbeatAndReconnect(t *testing.T) {
	srv, cfg := startServer(t)
	tok1 := session(t, srv, cfg, "alice", "agent-1")

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/executions/acquire", bytes.NewBufferString(`{"resource":"event:ars-che","action":"purchase"}`))
	req.Header.Set("Authorization", "Bearer "+tok1)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var granted struct {
		ID               string `json:"execution_id"`
		HeartbeatAfterMs int64  `json:"heartbeat_after_ms"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&granted); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("acquire %d", resp.StatusCode)
	}
	if granted.HeartbeatAfterMs != cfg.HeartbeatInterval.Milliseconds() {
		t.Fatalf("heartbeat_after_ms=%d want %d", granted.HeartbeatAfterMs, cfg.HeartbeatInterval.Milliseconds())
	}

	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/v1/executions/"+granted.ID+"/heartbeat", nil)
	req.Header.Set("Authorization", "Bearer "+tok1)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var beat struct {
		RenewCount int `json:"renew_count"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&beat); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("heartbeat %d", resp.StatusCode)
	}
	if beat.RenewCount < 1 {
		t.Fatalf("heartbeat renew_count=%d", beat.RenewCount)
	}

	tokResume := session(t, srv, cfg, "alice", "agent-1")
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/v1/executions/acquire", bytes.NewBufferString(`{"resource":"event:ars-che","action":"purchase"}`))
	req.Header.Set("Authorization", "Bearer "+tokResume)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var resumed struct {
		ID string `json:"execution_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&resumed); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("reconnect acquire want 200 got %d", resp.StatusCode)
	}
	if resumed.ID != granted.ID {
		t.Fatalf("reconnect want same execution %s got %s", granted.ID, resumed.ID)
	}
}

func TestHTTPConcurrentAcquire(t *testing.T) {
	srv, cfg := startServer(t)
	const n = 80
	tokens := make([]string, n)
	for i := 0; i < n; i++ {
		tokens[i] = session(t, srv, cfg, "alice", fmt.Sprintf("agent-%d", i))
	}
	var wg sync.WaitGroup
	wg.Add(n)
	codes := make([]int, n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/executions/acquire", bytes.NewBufferString(`{"resource":"event:big","action":"purchase"}`))
			req.Header.Set("Authorization", "Bearer "+tokens[i])
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			codes[i] = resp.StatusCode
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}()
	}
	wg.Wait()
	created, conflict := 0, 0
	for _, c := range codes {
		switch c {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			conflict++
		default:
			t.Fatalf("unexpected status %d", c)
		}
	}
	if created != 1 {
		t.Fatalf("created=%d conflict=%d want created=1", created, conflict)
	}
}
