// Package e2e asserts the Harchester Compose/local demo over HTTP.
// It does not import internal/. Skip unless DEMO_E2E=1 and the stack is up.
package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestDemoSmokeAndSwarm(t *testing.T) {
	if os.Getenv("DEMO_E2E") == "" {
		t.Skip("DEMO_E2E not set")
	}
	club := getenv("HARCHESTER_URL", "http://127.0.0.1:8100")
	edge := getenv("SIMTIX_EDGE_URL", "http://127.0.0.1:8091")
	lab := getenv("LOADLAB_URL", "http://127.0.0.1:8120")
	gw := getenv("BRUISER_URL", "http://127.0.0.1:8080")
	adminSecret := getenv("DEMO_ADMIN_SECRET", "demo-admin-dev")
	opSecret := getenv("BRUISER_OPERATOR_SECRET", "operator-secret-dev")

	home, err := http.Get(club + "/")
	if err != nil {
		t.Fatalf("club: %v", err)
	}
	body, _ := io.ReadAll(io.LimitReader(home.Body, 1<<20))
	_ = home.Body.Close()
	if home.StatusCode != 200 {
		t.Fatalf("club %d", home.StatusCode)
	}
	if bytes.Contains(bytes.ToLower(body), []byte("bruiser")) {
		t.Fatal("club home must not mention bruiser")
	}

	reset(t, gw+"/v1/operator/reset-executions", "X-Bruiser-Operator-Secret", opSecret)
	reset(t, getenv("SIMTIX_ORIGIN_URL", "http://127.0.0.1:8090")+"/_admin/reset", "X-Demo-Admin-Secret", adminSecret)

	payload, _ := json.Marshal(map[string]any{
		"membership_number": "1001234",
		"password":          "password",
		"agents":            20,
		"spawn_per_s":       20,
		"retry_window_sec":  6,
		"preset":            "single",
	})
	req, _ := http.NewRequest(http.MethodPost, lab+"/run", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Demo-Admin-Secret", adminSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("run %d", resp.StatusCode)
	}

	deadline := time.Now().Add(20 * time.Second)
	var summary map[string]any
	for time.Now().Before(deadline) {
		st := getJSON(t, lab+"/status", "X-Demo-Admin-Secret", adminSecret)
		if done, _ := st["done"].(bool); done {
			summary, _ = st["summary"].(map[string]any)
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if summary == nil {
		t.Fatal("swarm did not finish")
	}
	if n := asInt(summary["authenticated"]); n != 20 {
		t.Fatalf("authenticated=%v", summary["authenticated"])
	}
	if n := asInt(summary["allowed"]); n != 1 {
		t.Fatalf("allowed=%v want 1", summary["allowed"])
	}
	if n := asInt(summary["orders"]); n != 1 {
		t.Fatalf("orders=%v want 1", summary["orders"])
	}
	_ = edge
}

func reset(t *testing.T, url, header, secret string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, nil)
	req.Header.Set(header, secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func getJSON(t *testing.T, url, header, secret string) map[string]any {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set(header, secret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return out
}

func asInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return -1
	}
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
