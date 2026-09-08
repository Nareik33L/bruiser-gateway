package publicapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/check"
	pgstore "github.com/Nareik33L/bruiser-gateway/internal/store/postgres"
	"github.com/Nareik33L/bruiser-gateway/internal/testlab"
)

func TestAdminStatusAndAuditExport(t *testing.T) {
	api, srv, cfg, _ := testlab.GatewayAPI(t, testlab.ArsenalProfile(t))
	tok := session(t, srv, cfg, "alice", "agent-1")
	got := acquireJSON(t, srv, tok)
	if got.StatusCode != http.StatusCreated {
		t.Fatalf("acquire %d %s", got.StatusCode, got.Raw)
	}

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/admin/status", nil)
	req.Header.Set("X-Bruiser-Admin-Secret", cfg.AdminSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d %s", resp.StatusCode, b)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["merchant"] != cfg.MerchantID {
		t.Fatalf("merchant=%v", body["merchant"])
	}

	rep := check.Report{Overall: "PASS", Probes: []check.Probe{{Name: "lab", Pass: true}}}
	if err := api.PersistAuthorityCheck(context.Background(), rep, "req-1"); err != nil {
		t.Fatal(err)
	}
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/v1/admin/authority-check", nil)
	req.Header.Set("X-Bruiser-Admin-Secret", cfg.AdminSecret)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(b), "PASS") {
		t.Fatalf("last check %d %s", resp.StatusCode, b)
	}

	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/v1/admin/export", nil)
	req.Header.Set("X-Bruiser-Admin-Secret", cfg.AdminSecret)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(raw), "AUTHORITY_CHECK") {
		t.Fatalf("export %d %s", resp.StatusCode, raw)
	}

	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/admin", nil)
	req.Header.Set("X-Bruiser-Admin-Secret", cfg.AdminSecret)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	html, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(html), "Observed EAF") || !strings.Contains(string(html), "Queue depth") {
		t.Fatalf("admin page %d", resp.StatusCode)
	}
}

func TestAuditRetentionPurge(t *testing.T) {
	_, srv, cfg, _ := testlab.GatewayAPI(t, testlab.ArsenalProfile(t))
	url := cfg.DatabaseURL
	ctx := context.Background()
	store, err := pgstore.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.WriteAudit(ctx, cfg.MerchantID, "AUTHORITY_CHECK", "PASS", "old", nil); err != nil {
		t.Fatal(err)
	}
	_, err = store.Pool().Exec(ctx, `
		update audit_events set at = now() - interval '400 days'
		where merchant_id = $1 and request_id = 'old'`, cfg.MerchantID)
	if err != nil {
		t.Fatal(err)
	}
	n, err := store.PurgeAudit(ctx, cfg.MerchantID, time.Now().UTC().Add(-30*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("purged=%d want >=1", n)
	}
	ev, err := store.LastAudit(ctx, cfg.MerchantID, "AUDIT_PURGED")
	if err != nil {
		t.Fatal(err)
	}
	if ev.Type != "AUDIT_PURGED" {
		t.Fatalf("type=%s", ev.Type)
	}
	_ = srv
}
