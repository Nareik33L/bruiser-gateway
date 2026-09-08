package publicapi_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/Nareik33L/bruiser-gateway/internal/policy"
	"gopkg.in/yaml.v3"
)

func TestHTTPBoundedQueue(t *testing.T) {
	srv, cfg := startServer(t)
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

	tok1 := session(t, srv, cfg, "alice", "agent-1")
	tok2 := session(t, srv, cfg, "alice", "agent-2")
	tok3 := session(t, srv, cfg, "alice", "agent-3")

	acquire := func(tok string) (int, map[string]any) {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/executions/acquire",
			bytes.NewBufferString(`{"resource":"event:ars-che","action":"purchase"}`))
		req.Header.Set("Authorization", "Bearer "+tok)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var body map[string]any
		raw, _ := io.ReadAll(resp.Body)
		_ = json.Unmarshal(raw, &body)
		return resp.StatusCode, body
	}

	code, g := acquire(tok1)
	if code != http.StatusCreated {
		t.Fatalf("first %d %v", code, g)
	}
	code, q := acquire(tok2)
	if code != http.StatusAccepted || q["status"] != "QUEUED" {
		t.Fatalf("second want QUEUED got %d %v", code, q)
	}
	if q["position"] != float64(1) {
		t.Fatalf("position %v", q["position"])
	}
	code, b := acquire(tok3)
	if code != http.StatusConflict || b["status"] != "BUSY" {
		t.Fatalf("third want BUSY got %d %v", code, b)
	}

	exeID, _ := g["execution_id"].(string)
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/v1/executions/"+exeID+"/release", nil)
	req.Header.Set("Authorization", "Bearer "+tok1)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("release %d", resp.StatusCode)
	}

	waiterID, _ := q["execution_id"].(string)
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/v1/executions/"+waiterID, nil)
	req.Header.Set("Authorization", "Bearer "+tok2)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var promoted map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&promoted)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || promoted["status"] != "ACTIVE" {
		t.Fatalf("promoted %d %v", resp.StatusCode, promoted)
	}
}
