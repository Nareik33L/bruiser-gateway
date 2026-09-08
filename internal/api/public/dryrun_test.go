package publicapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/config"
	"github.com/Nareik33L/bruiser-gateway/internal/ops"
	"github.com/Nareik33L/bruiser-gateway/internal/policy"
	"github.com/Nareik33L/bruiser-gateway/internal/testlab"
	"gopkg.in/yaml.v3"
)

func TestDryRunNeverBlocks(t *testing.T) {
	_, srv, cfg, _ := testlab.GatewayWith(t, testlab.ArsenalProfile(t), func(c *config.Config) {
		c.Mode = ops.ModeDryRun
	})
	doc := policy.DefaultDocument(cfg.MerchantID)
	doc.Domains[0].Waiting = policy.Waiting{Mode: "bounded", MaxWaiters: 1}
	raw, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/v1/policy", bytes.NewReader(raw))
	req.Header.Set("X-Bruiser-Admin-Secret", cfg.AdminSecret)
	req.Header.Set("Content-Type", "application/yaml")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put policy %d", resp.StatusCode)
	}

	cAlice1, err := auth.IssueBoxOfficeSession(cfg.DevHMACSecret, "1001234", 0)
	if err != nil {
		t.Fatal(err)
	}
	cAlice2, err := auth.IssueBoxOfficeSession(cfg.DevHMACSecret, "1001234", 0)
	if err != nil {
		t.Fatal(err)
	}
	cBob, err := auth.IssueBoxOfficeSession(cfg.DevHMACSecret, "2000001", 0)
	if err != nil {
		t.Fatal(err)
	}

	authorize := func(cookie string) (int, http.Header, map[string]any) {
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
		return resp.StatusCode, resp.Header.Clone(), out
	}

	code, hdr, body := authorize(cAlice1)
	if code != http.StatusOK || body["status"] != "ALLOW" || hdr.Get("X-Bruiser-Dry-Run") != "WOULD_ALLOW" {
		t.Fatalf("alice1 %d %v %v", code, hdr.Get("X-Bruiser-Dry-Run"), body)
	}
	code, hdr, body = authorize(cAlice2)
	if code != http.StatusOK || body["status"] != "ALLOW" {
		t.Fatalf("alice2 must not be blocked: %d %v", code, body)
	}
	if hdr.Get("X-Bruiser-Dry-Run") != "WOULD_QUEUE" {
		t.Fatalf("alice2 would=%s body=%v", hdr.Get("X-Bruiser-Dry-Run"), body)
	}
	code, hdr, body = authorize(cBob)
	if code != http.StatusOK || hdr.Get("X-Bruiser-Dry-Run") != "WOULD_ALLOW" {
		t.Fatalf("bob must WOULD_ALLOW independently: %d %s %v", code, hdr.Get("X-Bruiser-Dry-Run"), body)
	}

	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/v1/admin/dry-run", nil)
	req.Header.Set("X-Bruiser-Admin-Secret", cfg.AdminSecret)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var report map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&report); err != nil {
		t.Fatal(err)
	}
	if report["would_allow"].(float64) < 2 || report["would_queue"].(float64) < 1 {
		t.Fatalf("dry-run report %+v", report)
	}

	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/v1/admin/status", nil)
	req.Header.Set("X-Bruiser-Admin-Secret", cfg.AdminSecret)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var status map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	execs, _ := status["executions"].([]any)
	if len(execs) != 0 {
		t.Fatalf("dry-run must not create real executions: %v", execs)
	}
}

func TestKillSwitchPassThrough(t *testing.T) {
	_, srv, cfg, _ := testlab.GatewayAPI(t, testlab.ArsenalProfile(t))
	cookie1, err := auth.IssueBoxOfficeSession(cfg.DevHMACSecret, "1001234", 0)
	if err != nil {
		t.Fatal(err)
	}
	cookie2, err := auth.IssueBoxOfficeSession(cfg.DevHMACSecret, "1001234", 0)
	if err != nil {
		t.Fatal(err)
	}
	authorize := func(cookie string) (int, map[string]any, string) {
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
		return resp.StatusCode, out, resp.Header.Get("X-Bruiser-Control")
	}
	code, body, _ := authorize(cookie1)
	if code != http.StatusOK || body["status"] != "ALLOW" {
		t.Fatalf("first %d %v", code, body)
	}
	code, body, _ = authorize(cookie2)
	if code != http.StatusConflict {
		t.Fatalf("second should BUSY before kill switch: %d %v", code, body)
	}

	req, _ := http.NewRequest(http.MethodPut, srv.URL+"/v1/admin/controls", bytes.NewBufferString(`{"enforcement":false,"updated_by":"test"}`))
	req.Header.Set("X-Bruiser-Admin-Secret", cfg.AdminSecret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put controls %d", resp.StatusCode)
	}

	code, body, ctl := authorize(cookie2)
	if code != http.StatusOK || body["status"] != "ALLOW" || ctl != "observe" {
		t.Fatalf("kill switch should observe-only (dry-run): %d ctl=%s %v", code, ctl, body)
	}
	if body["enforced"] == true {
		t.Fatalf("0%% must not enforce: %v", body)
	}
}
