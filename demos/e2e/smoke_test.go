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
	setEdge(t, "enforce", 100)
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
	setEdge(t, "enforce", 100)
	summary := runSwarm(t, map[string]any{
		"password":         "password",
		"agents":           agents,
		"spawn_per_s":      40,
		"retry_window_sec": 12,
		"preset":           "multi",
		"supporters":       supporters,
		"start_membership": 1000100,
	}, time.Duration(agents/8+40)*time.Second)
	if asInt(summary["authenticated"]) != agents {
		t.Fatalf("authenticated=%v want %d", summary["authenticated"], agents)
	}
	if asInt(summary["allowed"]) != supporters {
		t.Fatalf("allowed=%v want %d (one grant per supporter) summary=%v", summary["allowed"], supporters, summary)
	}
	orders := asInt(summary["orders"])
	if orders < supporters*4/5 || orders > supporters {
		t.Fatalf("orders=%v want ~%d summary=%v", summary["orders"], supporters, summary)
	}
}

func TestDemoTenCustomerSwarm(t *testing.T) {
	requireStack(t)
	per := 5
	if os.Getenv("DEMO_E2E_QUICK") != "" {
		per = 3
	}
	agents := 10 * per
	resetDemo(t)
	setEdge(t, "enforce", 100)
	summary := runSwarm(t, map[string]any{
		"password":          "password",
		"agents":            agents,
		"spawn_per_s":       agents,
		"retry_window_sec":  8,
		"preset":            "ten",
		"membership_number": "9999999",
	}, time.Duration(agents/5+30)*time.Second)
	if asInt(summary["authenticated"]) != agents {
		t.Fatalf("authenticated=%v want %d", summary["authenticated"], agents)
	}
	if asInt(summary["allowed"]) != 10 {
		t.Fatalf("allowed=%v want 10 (one grant per customer)", summary["allowed"])
	}
	if asInt(summary["orders"]) != 10 {
		t.Fatalf("orders=%v want 10", summary["orders"])
	}
	if asInt(summary["busy"]) < agents-10 {
		t.Fatalf("busy=%v want at least %d", summary["busy"], agents-10)
	}
	st := getJSON(t, getenv("LOADLAB_URL", "http://127.0.0.1:8120")+"/status", "X-Demo-Admin-Secret", getenv("DEMO_ADMIN_SECRET", "demo-admin-dev"))
	events, _ := st["events"].([]any)
	seen := map[string]bool{}
	for _, raw := range events {
		ev, _ := raw.(map[string]any)
		if ev["type"] != "agent" {
			continue
		}
		if m, _ := ev["membership"].(string); m != "" {
			seen[m] = true
		}
		if ev["membership"] == "9999999" {
			t.Fatal("ten preset must not use the single membership field")
		}
	}
	if len(seen) < 8 {
		t.Fatalf("live feed should show distinct memberships, got %d in %v", len(seen), seen)
	}
}

func TestDemoOffBurnsSeats(t *testing.T) {
	requireStack(t)
	per := 6
	if os.Getenv("DEMO_E2E_QUICK") != "" {
		per = 5
	}
	agents := 10 * per
	resetDemo(t)
	setEdge(t, "off", 0)
	t.Cleanup(func() { setEdge(t, "enforce", 100) })
	before := simtixStats(t)
	summary := runSwarm(t, map[string]any{
		"password":         "password",
		"agents":           agents,
		"spawn_per_s":      agents,
		"retry_window_sec": 6,
		"preset":           "ten",
	}, time.Duration(agents/5+40)*time.Second)
	after := simtixStats(t)
	if asInt(summary["orders"]) < 20 {
		t.Fatalf("off 10×N should complete many orders, got %v summary=%v", summary["orders"], summary)
	}
	if asInt(after["sold"]) <= asInt(before["sold"]) {
		t.Fatalf("sold did not rise: before=%v after=%v", before["sold"], after["sold"])
	}
	if asInt(after["available"]) >= asInt(before["available"]) {
		t.Fatalf("seats remaining did not drop: before=%v after=%v", before["available"], after["available"])
	}
	if asInt(summary["busy"]) != 0 {
		t.Fatalf("off must not treat origin 409 as Bruiser BUSY: busy=%v orders=%v denied=%v", summary["busy"], summary["orders"], summary["denied"])
	}
	if asInt(summary["allowed"]) < asInt(summary["orders"]) {
		t.Fatalf("every order needs an allow, allowed=%v orders=%v", summary["allowed"], summary["orders"])
	}
}

func TestDemoOffSingleBurnsPastAccountCap(t *testing.T) {
	requireStack(t)
	n := 80
	if os.Getenv("DEMO_E2E_QUICK") != "" {
		n = 25
	}
	resetDemo(t)
	setEdge(t, "off", 0)
	t.Cleanup(func() { setEdge(t, "enforce", 100) })
	before := simtixStats(t)
	summary := runSwarm(t, map[string]any{
		"membership_number": "1001234",
		"password":          "password",
		"agents":            n,
		"spawn_per_s":       n,
		"retry_window_sec":  8,
		"preset":            "single",
	}, time.Duration(n/4+40)*time.Second)
	after := simtixStats(t)
	orders := asInt(summary["orders"])
	if orders <= 4 {
		t.Fatalf("Off must lift origin per-account cap of 4; orders=%v summary=%v", orders, summary)
	}
	if orders < n/2 {
		t.Fatalf("Off single swarm should fill well past 4 tickets, orders=%v agents=%d summary=%v", orders, n, summary)
	}
	if asInt(after["sold"]) <= 4 {
		t.Fatalf("sold stuck at per-account cap: before=%v after=%v", before, after)
	}
	if asInt(after["available"]) >= asInt(before["available"])-4 {
		t.Fatalf("seats should drop by more than 4: before=%v after=%v", before["available"], after["available"])
	}
	if asInt(summary["busy"]) != 0 {
		t.Fatalf("Off must not report Bruiser BUSY: busy=%v", summary["busy"])
	}
}

func TestDemoResetClearsFeed(t *testing.T) {
	requireStack(t)
	resetDemo(t)
	setEdge(t, "enforce", 100)
	_ = runSwarm(t, map[string]any{
		"membership_number": "1001234",
		"password":          "password",
		"agents":            8,
		"spawn_per_s":       8,
		"retry_window_sec":  6,
		"preset":            "single",
	}, 25*time.Second)
	lab := getenv("LOADLAB_URL", "http://127.0.0.1:8120")
	adminSecret := getenv("DEMO_ADMIN_SECRET", "demo-admin-dev")
	before := getJSON(t, lab+"/status", "X-Demo-Admin-Secret", adminSecret)
	ev, _ := before["events"].([]any)
	if len(ev) == 0 {
		t.Fatal("expected live-feed events before reset")
	}
	resetDemo(t)
	after := getJSON(t, lab+"/status", "X-Demo-Admin-Secret", adminSecret)
	ev, _ = after["events"].([]any)
	if len(ev) != 0 {
		t.Fatalf("reset must clear live feed, got %d events: %v", len(ev), ev)
	}
	sum, _ := after["summary"].(map[string]any)
	if asInt(sum["denied"]) != 0 || asInt(sum["authenticated"]) != 0 {
		t.Fatalf("reset must clear summary, got %v", sum)
	}
}

func TestAuthorityCheckCertificate(t *testing.T) {
	requireStack(t)
	bin := getenv("BRUISER_BIN", "")
	if bin == "" {
		for _, c := range []string{"bin/bruiser", "../bin/bruiser", "../../bin/bruiser"} {
			if _, err := os.Stat(c); err == nil {
				bin = c
				break
			}
		}
	}
	if bin == "" {
		bin = "bin/bruiser"
	}
	hmac := getenv("BRUISER_DEV_HMAC_SECRET", "dev-secret-change-me")
	cmd := exec.Command(bin, "authority-check",
		"--edge", getenv("SIMTIX_EDGE_URL", "http://127.0.0.1:8091"),
		"--origin", getenv("SIMTIX_ORIGIN_URL", "http://127.0.0.1:8090"),
		"--control", getenv("BRUISER_URL", "http://127.0.0.1:8080"),
		"--admin", getenv("BRUISER_ADMIN_URL", "http://127.0.0.1:8082"),
		"--membership", "1001234", "--event", "hfc-ars",
		"--hmac-secret", hmac, "--json")
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
	reset(t, getenv("LOADLAB_URL", "http://127.0.0.1:8120")+"/reset", "X-Demo-Admin-Secret", adminSecret)
	setEdge(t, "enforce", 100)
}

func setEdge(t *testing.T, mode string, percent int) {
	t.Helper()
	edge := getenv("SIMTIX_EDGE_URL", "http://127.0.0.1:8091")
	body, _ := json.Marshal(map[string]any{"mode": mode, "percent": percent})
	req, _ := http.NewRequest(http.MethodPost, edge+"/_edge/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Demo-Admin-Secret", getenv("DEMO_ADMIN_SECRET", "demo-admin-dev"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("edge config %d", resp.StatusCode)
	}
	cap := 4
	if mode == "off" {
		cap = 0
	}
	setOriginCap(t, cap)
}

func setOriginCap(t *testing.T, limit int) {
	t.Helper()
	origin := getenv("SIMTIX_ORIGIN_URL", "http://127.0.0.1:8090")
	body, _ := json.Marshal(map[string]int{"limit": limit})
	req, _ := http.NewRequest(http.MethodPut, origin+"/_admin/per-account-cap", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Demo-Admin-Secret", getenv("DEMO_ADMIN_SECRET", "demo-admin-dev"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("origin cap %d", resp.StatusCode)
	}
}

func simtixStats(t *testing.T) map[string]any {
	t.Helper()
	return getJSON(t, getenv("SIMTIX_ORIGIN_URL", "http://127.0.0.1:8090")+"/_admin/stats", "X-Demo-Admin-Secret", getenv("DEMO_ADMIN_SECRET", "demo-admin-dev"))
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
