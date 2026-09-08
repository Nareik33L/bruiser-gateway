package publicapi_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Nareik33L/bruiser-gateway/internal/simtix"
	"github.com/Nareik33L/bruiser-gateway/internal/testlab"
	bruiser "github.com/Nareik33L/bruiser-gateway/sdk/go"
)

func TestTakeControlHandoffScenario(t *testing.T) {
	_, bruiserSrv, cfg, signer := testlab.GatewayAPI(t, testlab.ArsenalProfile(t))
	fences := &bruiser.FenceCache{}
	origin := simtix.New(simtix.Config{
		HMACSecret:       cfg.DevHMACSecret,
		OriginSecret:     cfg.OriginSecret,
		Seats:            8,
		RequireExecution: true,
		VerifyExecution: func(tok string) error {
			claims, err := bruiser.Verify(tok, signer.Public)
			if err != nil {
				return err
			}
			if err := fences.Accept(claims.Domain, claims.Fence); err != nil {
				return simtix.ErrStaleFence
			}
			return nil
		},
	})
	sim := httptest.NewServer(origin.Handler())
	t.Cleanup(sim.Close)

	agentTok := sessionAs(t, bruiserSrv, cfg, "alice", "agent", "shopping-agent")
	browserTok := sessionAs(t, bruiserSrv, cfg, "alice", "browser", "alice-tab")

	agentExe := acquireJSON(t, bruiserSrv, agentTok)
	if agentExe.StatusCode != http.StatusCreated {
		t.Fatalf("agent acquire %d %s", agentExe.StatusCode, agentExe.Raw)
	}

	hold := postJSON(t, sim.URL+"/api/events/ars-che/holds", `{"seats":1}`, map[string]string{
		"X-Bruiser-Execution":     agentExe.Token,
		"X-Bruiser-Origin-Secret": cfg.OriginSecret,
	})
	if hold.StatusCode != http.StatusCreated {
		t.Fatalf("agent hold %d %s", hold.StatusCode, hold.Raw)
	}

	busy := acquireJSON(t, bruiserSrv, browserTok)
	if busy.StatusCode != http.StatusConflict {
		t.Fatalf("browser acquire want 409 got %d %s", busy.StatusCode, busy.Raw)
	}
	if !busy.CanPreempt {
		t.Fatalf("can_preempt=false: %s", busy.Raw)
	}

	handoff := postJSON(t, bruiserSrv.URL+"/v1/executions/"+agentExe.ID+"/handoff", `{"mode":"preempt"}`, map[string]string{
		"Authorization": "Bearer " + browserTok,
	})
	if handoff.StatusCode != http.StatusCreated {
		t.Fatalf("handoff %d %s", handoff.StatusCode, handoff.Raw)
	}
	if handoff.Fence <= agentExe.Fence {
		t.Fatalf("fence %d → %d", agentExe.Fence, handoff.Fence)
	}

	renew := postJSON(t, bruiserSrv.URL+"/v1/executions/"+agentExe.ID+"/renew", "", map[string]string{
		"Authorization": "Bearer " + agentTok,
	})
	if renew.StatusCode != http.StatusGone {
		t.Fatalf("agent renew want 410 got %d %s", renew.StatusCode, renew.Raw)
	}
	if renew.Reason != "HANDED_OFF" {
		t.Fatalf("reason=%s %s", renew.Reason, renew.Raw)
	}
	if renew.SuccessorID != handoff.ID {
		t.Fatalf("successor %s want %s", renew.SuccessorID, handoff.ID)
	}

	intro := postJSON(t, bruiserSrv.URL+"/v1/introspect", `{"token":`+jsonQuote(agentExe.Token)+`}`, nil)
	if intro.StatusCode != http.StatusOK || intro.Active {
		t.Fatalf("introspect old token active=%v %d %s", intro.Active, intro.StatusCode, intro.Raw)
	}

	browserHold := postJSON(t, sim.URL+"/api/events/ars-che/holds", `{"seats":1}`, map[string]string{
		"X-Bruiser-Execution":     handoff.Token,
		"X-Bruiser-Origin-Secret": cfg.OriginSecret,
	})
	if browserHold.StatusCode != http.StatusCreated {
		t.Fatalf("browser hold %d %s", browserHold.StatusCode, browserHold.Raw)
	}

	stale := postJSON(t, sim.URL+"/api/orders", `{"event_id":"ars-che","hold_id":"`+hold.HoldID+`"}`, map[string]string{
		"X-Bruiser-Execution":     agentExe.Token,
		"X-Bruiser-Origin-Secret": cfg.OriginSecret,
	})
	if stale.StatusCode != http.StatusForbidden {
		t.Fatalf("stale purchase want 403 got %d %s", stale.StatusCode, stale.Raw)
	}

	buy := postJSON(t, sim.URL+"/api/orders", `{"event_id":"ars-che","hold_id":"`+browserHold.HoldID+`"}`, map[string]string{
		"X-Bruiser-Execution":     handoff.Token,
		"X-Bruiser-Origin-Secret": cfg.OriginSecret,
	})
	if buy.StatusCode != http.StatusCreated {
		t.Fatalf("browser purchase %d %s", buy.StatusCode, buy.Raw)
	}
}

func TestRevokeThenReacquire(t *testing.T) {
	srv, cfg := startServer(t)
	agentTok := sessionAs(t, srv, cfg, "alice", "agent", "bot")
	browserTok := sessionAs(t, srv, cfg, "alice", "browser", "tab")
	got := acquireJSON(t, srv, agentTok)
	if got.StatusCode != http.StatusCreated {
		t.Fatalf("acquire %d %s", got.StatusCode, got.Raw)
	}
	rev := postJSON(t, srv.URL+"/v1/executions/"+got.ID+"/revoke", `{"reason":"customer_stop"}`, map[string]string{
		"Authorization": "Bearer " + browserTok,
	})
	if rev.StatusCode != http.StatusOK {
		t.Fatalf("revoke %d %s", rev.StatusCode, rev.Raw)
	}
	again := acquireJSON(t, srv, browserTok)
	if again.StatusCode != http.StatusCreated {
		t.Fatalf("reacquire %d %s", again.StatusCode, again.Raw)
	}
}

type exeBody struct {
	StatusCode  int
	Raw         string
	ID          string
	Token       string
	Fence       int64
	CanPreempt  bool
	Reason      string
	SuccessorID string
	Active      bool
	HoldID      string
}

func acquireJSON(t *testing.T, srv *httptest.Server, tok string) exeBody {
	t.Helper()
	return postJSON(t, srv.URL+"/v1/executions/acquire", `{"resource":"event:ars-che","action":"purchase"}`, map[string]string{
		"Authorization": "Bearer " + tok,
	})
}

func postJSON(t *testing.T, url, body string, headers map[string]string) exeBody {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req, err := http.NewRequest(http.MethodPost, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := exeBody{StatusCode: resp.StatusCode, Raw: string(raw)}
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
	if v, ok := m["can_preempt"].(bool); ok {
		out.CanPreempt = v
	}
	if v, ok := m["reason"].(string); ok {
		out.Reason = v
	}
	if v, ok := m["successor_id"].(string); ok {
		out.SuccessorID = v
	}
	if v, ok := m["active"].(bool); ok {
		out.Active = v
	}
	if v, ok := m["hold_id"].(string); ok {
		out.HoldID = v
	}
	return out
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
