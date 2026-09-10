package main

import (
	"strings"
	"testing"
)

func TestDashboardPerCustomerLimitControl(t *testing.T) {
	if !strings.Contains(dashboardHTML, `Per-customer limit`) {
		t.Fatal("presenter control label missing")
	}
	for _, want := range []string{`data-limit="off"`, `data-limit="dry-run"`, `data-limit="on"`} {
		if !strings.Contains(dashboardHTML, want) {
			t.Fatalf("missing %s", want)
		}
	}
	if !strings.Contains(dashboardHTML, `On = one customer, one execution. Off = no limit.`) {
		t.Fatal("short hint missing")
	}
	if !strings.Contains(dashboardHTML, `Rollout % (advanced)`) {
		t.Fatal("percent should stay as advanced, not primary")
	}
}

func TestAdminConsoleIvoryRustIdentity(t *testing.T) {
	css := pageShell("Operations", dashboardHTML)
	if strings.Contains(css, "#d6ff4a") || strings.Contains(css, "--lime") {
		t.Fatal("admin console must not use neon lime")
	}
	if !strings.Contains(css, "#f3eee4") {
		t.Fatal("expected warm ivory background")
	}
	if !strings.Contains(css, "#1c1917") {
		t.Fatal("expected charcoal ink")
	}
	if !strings.Contains(css, "#b44a32") {
		t.Fatal("expected rust accent")
	}
	if !strings.Contains(css, `class="mark"`) {
		t.Fatal("expected rust brand mark")
	}
}
