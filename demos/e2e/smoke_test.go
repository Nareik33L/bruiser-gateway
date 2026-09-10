// Package e2e asserts the Harchester Compose/local demo over HTTP.
// It does not import internal/. Skip unless DEMO_E2E=1 and the stack is up.
package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

func TestClubHomeHidesBruiser(t *testing.T) {
	requireStack(t)
	club := getenv("HARCHESTER_URL", "http://127.0.0.1:8100")
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
}

func TestDemoSingleSupporterSwarm(t *testing.T) {
	requireStack(t)
	n := envInt("DEMO_E2E_AGENTS", 200)
	if os.Getenv("DEMO_E2E_QUICK") != "" {
		n = 20
	}
	resetDemo(t)
	summary := runSwarm(t, map[string]any{
		"membership_number": "1001234",
		"password":          "password",
		"agents":            n,
		"spawn_per_s":       n,
		"retry_window_sec":  8,
		"preset":            "single",
	}, time.Duration(n/10+25)*time.Second)
	if asInt(summary["authenticated"]) != n {
		t.Fatalf("authenticated=%v want %d", summary["authenticated"], n)
	}
	if asInt(summary["allowed"]) != 1 {
		t.Fatalf("allowed=%v want 1", summary["allowed"])
	}
	if asInt(summary["orders"]) != 1 {
		t.Fatalf("orders=%v want 1", summary["orders"])
	}
}

func TestDemoMultiSupporterSwarm(t *testing.T) {
	requireStack(t)
	supporters, agents := 50, 200
	if os.Getenv("DEMO_E2E_QUICK") != "" {
		supporters, agents = 4, 16
	}
	resetDemo(t)
	summary := runSwarm(t, map[string]any{
		"password":         "password",
		"agents":           agents,
		"spawn_per_s":      agents,
		"retry_window_sec": 8,
		"preset":           "multi",
		"supporters":       supporters,
		"start_membership": 1000100,
	}, time.Duration(agents/10+30)*time.Second)
	if asInt(summary["authenticated"]) != agents {
		t.Fatalf("authenticated=%v want %d", summary["authenticated"], agents)
	}
	if asInt(summary["allowed"]) != supporters {
		t.Fatalf("allowed=%v want %d (one grant per supporter)", summary["allowed"], supporters)
	}
	if asInt(summary["orders"]) != supporters {
		t.Fatalf("orders=%v want %d", summary["orders"], supporters)
	}
}

func TestAuthorityCheckCertificate(t *testing.T) {
	requireStack(t)
	bin := getenv("BRUISER_BIN", "bin/bruiser")
	cmd := exec.Command(bin, "authority-check",
		"--edge", getenv("SIMTIX_EDGE_URL", "http://127.0.0.1:8091"),
		"--origin", getenv("SIMTIX_ORIGIN_URL", "http://127.0.0.1:8090"),
		"--membership", "1001234", "--event", "hfc-ars", "--json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("authority-check: %v\n%s", err, out)
	}
	var rep struct {
		Overall string `json:"overall"`
		Probes  []struct {
			Name string `json:"name"`
			Pass bool   `json:"pass"`
		} `json:"probes"`
	}
	if err := json.Unmarshal(out, &rep); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if rep.Overall != "PASS" {
		t.Fatalf("overall %s", rep.Overall)
	}
	if len(rep.Probes) == 0 {
		t.Fatal("expected probes on the certificate")
	}
}

func requireStack(t *testing.T) {
	t.Helper()
	if os.Getenv("DEMO_E2E") == "" {
		t.Skip("DEMO_E2E not set")
	}
}

func resetDemo(t *testing.T) {
	t.Helper()
	adminSecret := getenv("DEMO_ADMIN_SECRET", "demo-admin-dev")
	opSecret := getenv("BRUISER_OPERATOR_SECRET", "operator-secret-dev")
	reset(t, getenv("BRUISER_URL", "http://127.0.0.1:8080")+"/v1/operator/reset-executions", "X-Bruiser-Operator-Secret", opSecret)
	reset(t, getenv("SIMTIX_ORIGIN_URL", "http://127.0.0.1:8090")+"/_admin/reset", "X-Demo-Admin-Secret", adminSecret)
}

func runSwarm(t *testing.T, payload map[string]any, wait time.Duration) map[string]any {
	t.Helper()
	lab := getenv("LOADLAB_URL", "http://127.0.0.1:8120")
	adminSecret := getenv("DEMO_ADMIN_SECRET", "demo-admin-dev")
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, lab+"/run", bytes.NewReader(body))
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
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		st := getJSON(t, lab+"/status", "X-Demo-Admin-Secret", adminSecret)
		if done, _ := st["done"].(bool); done {
			summary, _ := st["summary"].(map[string]any)
			if summary == nil {
				t.Fatal("swarm finished with no summary")
			}
			return summary
		}
		time.Sleep(400 * time.Millisecond)
	}
	t.Fatal("swarm did not finish")
	return nil
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

func envInt(k string, d int) int {
	if v := os.Getenv(k); v != "" {
		n, err := strconv.Atoi(v)
		if err == nil && n > 0 {
			return n
		}
	}
	return d
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
