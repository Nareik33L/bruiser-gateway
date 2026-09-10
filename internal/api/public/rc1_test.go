package publicapi_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/policy"
	"github.com/Nareik33L/bruiser-gateway/internal/testlab"
	"gopkg.in/yaml.v3"
)

func TestRC1IdentityForgedAndExpiry(t *testing.T) {
	srv, cfg := testlab.Gateway(t, testlab.ArsenalProfile(t))
	forged := "not-a-jwt"
	if code := sessionCode(t, srv.URL, forged); code != http.StatusUnauthorized {
		t.Fatalf("forged assertion %d", code)
	}
	expired, err := auth.IssueDevAssertion(cfg.DevHMACSecret, "alice", -time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	if code := sessionCode(t, srv.URL, expired); code != http.StatusUnauthorized {
		t.Fatalf("expired assertion %d", code)
	}
}

func TestRC1SessionRevokeLosesAuthority(t *testing.T) {
	srv, cfg := testlab.Gateway(t, testlab.ArsenalProfile(t))
	tok := mustAgentSession(t, srv.URL, cfg.DevHMACSecret, "alice", "agent-1")
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/sessions/logout", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout %d", resp.StatusCode)
	}
	code, _, _ := postAuth(t, srv.URL+"/v1/executions/acquire", `{"resource":"event:ars-che","action":"hold"}`, tok)
	if code != http.StatusUnauthorized {
		t.Fatalf("revoked session still acquired %d", code)
	}
}

func TestRC1CanonicalResourceOneDomain(t *testing.T) {
	srv, cfg := testlab.Gateway(t, testlab.ArsenalProfile(t))
	tok := mustAgentSession(t, srv.URL, cfg.DevHMACSecret, "alice", "canon")
	code, _, body := postAuth(t, srv.URL+"/v1/executions/acquire", `{"resource":"EVENT:ARS-CHE/","action":"hold"}`, tok)
	if code != http.StatusCreated {
		t.Fatalf("first %d %v", code, body)
	}
	code, _, body = postAuth(t, srv.URL+"/v1/executions/acquire", `{"resource":"event:ars-che","action":"hold"}`, tok)
	if code != http.StatusOK {
		t.Fatalf("variant should resume same execution %d %v", code, body)
	}
}

func TestRC1NineResourceVariantsOneActive(t *testing.T) {
	srv, cfg := testlab.Gateway(t, testlab.ArsenalProfile(t))
	variants := []string{
		"ticket:cupfinal",
		"ticket:cupfinal.",
		"ticket:cupfinal-",
		"ticket:cupfinal_",
		"ticket:cupfinal!",
		"ticket:cupfinal@",
		"ticket:cupfinal   ",
		"ticket:cupfinal\u2010",
		"ticket:cupfinal\u2013",
	}
	created, denied := 0, 0
	exe := ""
	for i, res := range variants {
		tok := mustAgentSession(t, srv.URL, cfg.DevHMACSecret, "7770001", fmt.Sprintf("agent-%d", i))
		payload := fmt.Sprintf(`{"resource":%q,"action":"hold"}`, res)
		code, raw, body := postAuth(t, srv.URL+"/v1/executions/acquire", payload, tok)
		id, _ := body["execution_id"].(string)
		switch code {
		case http.StatusCreated:
			created++
			if exe == "" {
				exe = id
			} else if id != "" && id != exe {
				t.Fatalf("%q minted second execution %s vs %s", res, id, exe)
			}
		case http.StatusConflict, http.StatusForbidden:
			denied++
		case http.StatusOK:
			if exe != "" && id != "" && id != exe {
				t.Fatalf("%q resumed different execution %s vs %s", res, id, exe)
			}
			denied++
		default:
			t.Fatalf("%q → %d %s (want 1 ACTIVE and 8 denials)", res, code, raw)
		}
	}
	if created != 1 || denied != 8 {
		t.Fatalf("created=%d denied=%d want 1 ACTIVE and 8 denials", created, denied)
	}
}

func TestRC1CatalogueRejectsUnknown(t *testing.T) {
	p := testlab.ArsenalProfile(t)
	p.Resources = []string{"event:ars-che"}
	srv, cfg := testlab.Gateway(t, p)
	tok := mustAgentSession(t, srv.URL, cfg.DevHMACSecret, "alice", "cat")
	code, raw, _ := postAuth(t, srv.URL+"/v1/executions/acquire", `{"resource":"ticket:cupfinal","action":"hold"}`, tok)
	if code != http.StatusBadRequest {
		t.Fatalf("unknown resource %d %s", code, raw)
	}
	code, raw, _ = postAuth(t, srv.URL+"/v1/executions/acquire", `{"resource":"event:ars-che.","action":"hold"}`, tok)
	if code != http.StatusCreated {
		t.Fatalf("catalogue fold %d %s", code, raw)
	}
}

func TestRC1MalformedResourceRejected(t *testing.T) {
	srv, cfg := testlab.Gateway(t, testlab.ArsenalProfile(t))
	tok := mustAgentSession(t, srv.URL, cfg.DevHMACSecret, "alice", "bad")
	code, _, _ := postAuth(t, srv.URL+"/v1/executions/acquire", `{"resource":"event:../x","action":"hold"}`, tok)
	if code != http.StatusBadRequest && code != http.StatusForbidden {
		t.Fatalf("malformed resource %d", code)
	}
}

func TestRC1AuthorizeStripsOriginSecret(t *testing.T) {
	srv, cfg := testlab.Gateway(t, testlab.ArsenalProfile(t))
	cookie, err := auth.IssueBoxOfficeSession(cfg.DevHMACSecret, "1001234", 0)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/authorize", bytes.NewBufferString(`{"method":"POST","path":"/api/events/ars-che/holds"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Bruiser-Edge-Secret", cfg.EdgeSecret)
	req.Header.Set("Cookie", "boxoffice_session="+cookie)
	req.Header.Set("X-Bruiser-Origin-Secret", "client-supplied")
	req.Header.Set("X-Bruiser-Customer", "spoof")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("X-Bruiser-Origin-Secret") != "" {
		t.Fatal("authorize leaked origin secret")
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("authorize %d %s", resp.StatusCode, b)
	}
}

func TestRC1ReplayIdempotencyKey(t *testing.T) {
	srv, cfg := testlab.Gateway(t, testlab.ArsenalProfile(t))
	tok := mustAgentSession(t, srv.URL, cfg.DevHMACSecret, "alice", "replay")
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/executions/acquire", bytes.NewBufferString(`{"resource":"event:ars-che","action":"hold"}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "idem-1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("first %d", resp.StatusCode)
	}
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/v1/executions/acquire", bytes.NewBufferString(`{"resource":"event:ars-che","action":"hold"}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "idem-1")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("replay want 409 got %d", resp.StatusCode)
	}
}

func TestRC1ExecutionBudget(t *testing.T) {
	lab := testlab.Start(t, testlab.ArsenalProfile(t), nil)
	doc := policy.DefaultDocument(lab.Cfg.MerchantID)
	doc.Domains[0].Budget.MaxOps = 1
	raw, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPut, lab.Server.URL+"/v1/policy", bytes.NewReader(raw))
	req.Header.Set("X-Bruiser-Admin-Secret", lab.Cfg.AdminSecret)
	req.Header.Set("Content-Type", "application/yaml")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("policy %d", resp.StatusCode)
	}
	cookie, err := auth.IssueBoxOfficeSession(lab.Cfg.DevHMACSecret, "budget-1", 0)
	if err != nil {
		t.Fatal(err)
	}
	authz := func() int {
		req, _ := http.NewRequest(http.MethodPost, lab.Server.URL+"/v1/authorize", bytes.NewBufferString(`{"method":"POST","path":"/api/events/ars-che/holds"}`))
		req.Header.Set("X-Bruiser-Edge-Secret", lab.Cfg.EdgeSecret)
		req.Header.Set("Cookie", "boxoffice_session="+cookie)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if code := authz(); code != http.StatusOK {
		t.Fatalf("first authorize %d", code)
	}
	if code := authz(); code != http.StatusConflict {
		t.Fatalf("budget want 409 got %d", code)
	}
}

func sessionCode(t *testing.T, gw, assertion string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, gw+"/v1/sessions", bytes.NewBufferString(`{"principal":{"type":"agent","id":"p"}}`))
	req.Header.Set("Authorization", "Bearer "+assertion)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode
}

func mustAgentSession(t *testing.T, gw, hmac, customer, principal string) string {
	t.Helper()
	assertion, err := auth.IssueDevAssertion(hmac, customer, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPost, gw+"/v1/sessions", bytes.NewBufferString(`{"principal":{"type":"agent","id":"`+principal+`"}}`))
	req.Header.Set("Authorization", "Bearer "+assertion)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Token string `json:"session_token"`
	}
	b, _ := io.ReadAll(resp.Body)
	if json.Unmarshal(b, &out) != nil || out.Token == "" {
		t.Fatalf("session %d %s", resp.StatusCode, b)
	}
	return out.Token
}

func postAuth(t *testing.T, url, body, tok string) (int, string, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp.StatusCode, string(raw), m
}
