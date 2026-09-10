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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/api/public"
	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/config"
	"github.com/Nareik33L/bruiser-gateway/internal/id"
	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
	pgstore "github.com/Nareik33L/bruiser-gateway/internal/store/postgres"
)

// TestThreeGatewaysOneGrant is the M2 thesis check: multiple gateway
// processes, many agents, one customer, one resource, never two GRANTs.
func TestThreeGatewaysOneGrant(t *testing.T) {
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

	const nodes = 3
	srvs := make([]*httptest.Server, nodes)
	stores := make([]*pgstore.Store, nodes)
	for i := 0; i < nodes; i++ {
		st, err := pgstore.Connect(ctx, url)
		if err != nil {
			t.Fatal(err)
		}
		stores[i] = st
		t.Cleanup(st.Close)
		h := publicapi.New(cfg, st, signer, slog.New(slog.NewTextHandler(io.Discard, nil)), merchant.Empty(cfg.MerchantID))
		srvs[i] = httptest.NewServer(h)
		t.Cleanup(srvs[i].Close)
	}

	const agents = 400
	tokens := make([]string, agents)
	for i := 0; i < agents; i++ {
		tokens[i] = mustSession(t, srvs[i%nodes], cfg, "alice", fmt.Sprintf("agent-%d", i))
	}

	var granted, busy, unavailable atomic.Int64
	var wg sync.WaitGroup
	wg.Add(agents)
	for i := 0; i < agents; i++ {
		i := i
		go func() {
			defer wg.Done()
			srv := srvs[i%nodes]
			req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/executions/acquire",
				bytes.NewBufferString(`{"resource":"event:final","action":"purchase"}`))
			req.Header.Set("Authorization", "Bearer "+tokens[i])
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			switch resp.StatusCode {
			case http.StatusCreated:
				granted.Add(1)
			case http.StatusConflict:
				busy.Add(1)
			case http.StatusServiceUnavailable:
				unavailable.Add(1)
			default:
				t.Errorf("status %d", resp.StatusCode)
			}
		}()
	}
	wg.Wait()

	g, b, u := granted.Load(), busy.Load(), unavailable.Load()
	if g != 1 {
		t.Fatalf("granted=%d busy=%d unavailable=%d want granted=1 across %d gateways", g, b, u, nodes)
	}

	var active int
	err = store.Pool().QueryRow(ctx, `
		select count(*) from executions
		where merchant_id = $1 and state = 'ACTIVE' and expires_at > now()`,
		cfg.MerchantID).Scan(&active)
	if err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("ACTIVE rows = %d want 1 (I1 exclusivity)", active)
	}
}

func mustSession(t *testing.T, srv *httptest.Server, cfg config.Config, customer, principal string) string {
	t.Helper()
	assertion, err := auth.IssueDevAssertion(cfg.DevHMACSecret, customer, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"principal":{"type":"agent","id":%q}}`, principal)
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
