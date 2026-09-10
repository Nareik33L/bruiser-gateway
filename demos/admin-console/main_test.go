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
