package publicapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/config"
	"github.com/Nareik33L/bruiser-gateway/internal/testlab"
)

func TestOperatorAbsentWhenUnset(t *testing.T) {
	_, srv, _, _ := testlab.GatewayWith(t, testlab.ArsenalProfile(t), func(cfg *config.Config) {
		cfg.OperatorSecret = ""
	})
	resp, err := http.Get(srv.URL + "/v1/operator/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unset secret must not mount operator, got %d", resp.StatusCode)
	}
}

func TestOperatorRejectsBadSecret(t *testing.T) {
	srv, _ := testlab.Gateway(t, testlab.ArsenalProfile(t))
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/operator/stats", nil)
	req.Header.Set("X-Bruiser-Operator-Secret", "wrong")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401 got %d", resp.StatusCode)
	}
}

func TestOperatorResetClearsBusy(t *testing.T) {
	srv, cfg := testlab.Gateway(t, testlab.ArsenalProfile(t))
	cookie1, err := auth.IssueBoxOfficeSession(cfg.DevHMACSecret, "1001234", 0)
	if err != nil {
		t.Fatal(err)
	}
	cookie2, err := auth.IssueBoxOfficeSession(cfg.DevHMACSecret, "1001234", 0)
	if err != nil {
		t.Fatal(err)
	}

	authorize := func(cookie string) (int, map[string]any) {
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
		return resp.StatusCode, out
	}

	code, body := authorize(cookie1)
	if code != http.StatusOK || body["status"] != "ALLOW" {
		t.Fatalf("first hold want ALLOW got %d %v", code, body)
	}
	code, body = authorize(cookie2)
	if code != http.StatusConflict || body["status"] != "BUSY" {
		t.Fatalf("second login want BUSY got %d %v", code, body)
	}

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/operator/reset-executions", nil)
	req.Header.Set("X-Bruiser-Operator-Secret", cfg.OperatorSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("reset %d %s", resp.StatusCode, b)
	}

	code, body = authorize(cookie2)
	if code != http.StatusOK || body["status"] != "ALLOW" {
		t.Fatalf("after reset want ALLOW got %d %v", code, body)
	}

	statsReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/operator/stats", nil)
	statsReq.Header.Set("X-Bruiser-Operator-Secret", cfg.OperatorSecret)
	statsResp, err := http.DefaultClient.Do(statsReq)
	if err != nil {
		t.Fatal(err)
	}
	defer statsResp.Body.Close()
	var stats map[string]any
	_ = json.NewDecoder(statsResp.Body).Decode(&stats)
	if stats["active_executions"].(float64) < 1 {
		t.Fatalf("expected an active execution after re-grant, got %v", stats)
	}

	auditReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/operator/audit?limit=50", nil)
	auditReq.Header.Set("X-Bruiser-Operator-Secret", cfg.OperatorSecret)
	auditResp, err := http.DefaultClient.Do(auditReq)
	if err != nil {
		t.Fatal(err)
	}
	defer auditResp.Body.Close()
	raw, _ := io.ReadAll(auditResp.Body)
	if !strings.Contains(string(raw), "ADMIN_RESET") && !strings.Contains(string(raw), "EXECUTION_REVOKED") {
		t.Fatalf("audit should retain revoke events, got %s", raw)
	}
}
