// Command torture hammers multiple in-process gateways against one Postgres
// and checks that a scarcity domain never has more than one ACTIVE execution.
package main

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
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/api/public"
	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/config"
	"github.com/Nareik33L/bruiser-gateway/internal/id"
	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
	pgstore "github.com/Nareik33L/bruiser-gateway/internal/store/postgres"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	cfg := config.Load()
	if v := os.Getenv("BRUISER_TEST_DATABASE_URL"); v != "" {
		cfg.DatabaseURL = v
	}
	ctx := context.Background()
	if err := pgstore.Migrate(ctx, cfg.DatabaseURL); err != nil {
		log.Error("migrate", "err", err)
		os.Exit(1)
	}
	store, err := pgstore.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("connect", "err", err)
		os.Exit(1)
	}
	defer store.Close()
	cfg.MerchantID = id.New("m")
	if err := store.EnsureMerchant(ctx, cfg.MerchantID, "torture", cfg.DevHMACSecret); err != nil {
		log.Error("merchant", "err", err)
		os.Exit(1)
	}
	key, err := store.EnsureSigningKey(ctx, cfg.MerchantID)
	if err != nil {
		log.Error("keys", "err", err)
		os.Exit(1)
	}
	signer := auth.Signer{KID: key.KID, MerchantID: cfg.MerchantID, Private: key.Private, Public: key.Public}

	nodes := 3
	agents := 2000
	srvs := make([]*httptest.Server, nodes)
	for i := 0; i < nodes; i++ {
		st, err := pgstore.Connect(ctx, cfg.DatabaseURL)
		if err != nil {
			log.Error("connect node", "err", err)
			os.Exit(1)
		}
		defer st.Close()
		h := publicapi.New(cfg, st, signer, slog.New(slog.NewTextHandler(io.Discard, nil)), merchant.Empty(cfg.MerchantID))
		srvs[i] = httptest.NewServer(h)
		defer srvs[i].Close()
	}

	tokens := make([]string, agents)
	for i := 0; i < agents; i++ {
		tokens[i] = session(srvs[i%nodes], cfg, fmt.Sprintf("agent-%d", i))
	}

	start := time.Now()
	var granted, busy atomic.Int64
	var wg sync.WaitGroup
	wg.Add(agents)
	for i := 0; i < agents; i++ {
		i := i
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodPost, srvs[i%nodes].URL+"/v1/executions/acquire",
				bytes.NewBufferString(`{"resource":"event:final","action":"purchase"}`))
			req.Header.Set("Authorization", "Bearer "+tokens[i])
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusCreated {
				granted.Add(1)
			} else if resp.StatusCode == http.StatusConflict {
				busy.Add(1)
			}
		}()
	}
	wg.Wait()

	var active int
	_ = store.Pool().QueryRow(ctx, `
		select count(*) from executions
		where merchant_id=$1 and state='ACTIVE' and expires_at>now()`,
		cfg.MerchantID).Scan(&active)

	fmt.Printf("nodes=%d agents=%d granted=%d busy=%d active=%d elapsed=%s\n",
		nodes, agents, granted.Load(), busy.Load(), active, time.Since(start).Truncate(time.Millisecond))
	if granted.Load() != 1 || active != 1 {
		fmt.Println("INVARIANT VIOLATION")
		os.Exit(1)
	}
	fmt.Println("I1 exclusivity: PASS")
}

func session(srv *httptest.Server, cfg config.Config, principal string) string {
	assertion, _ := auth.IssueDevAssertion(cfg.DevHMACSecret, "alice", time.Hour, nil)
	body := fmt.Sprintf(`{"principal":{"type":"agent","id":%q}}`, principal)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/sessions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+assertion)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()
	var out struct {
		Token string `json:"session_token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out.Token
}
