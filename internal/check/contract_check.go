package check

import (
	"fmt"
	"strings"
)

// ContractViolation explains why a PASS is dishonest.
func ContractViolation(p Probe, c Contract) string {
	if p.Status != "PASS" {
		return ""
	}
	if p.Evidence == nil {
		return p.Name + ": PASS without evidence"
	}
	ev := *p.Evidence
	if c.MinExchanges > 0 && len(ev.Exchanges) < c.MinExchanges {
		return fmt.Sprintf("%s: PASS with %d exchanges, want ≥%d", p.Name, len(ev.Exchanges), c.MinExchanges)
	}
	for _, h := range c.RequireHeaderPresent {
		if !ev.headerPresent(h) {
			return fmt.Sprintf("%s: PASS without sending %s", p.Name, h)
		}
	}
	for _, s := range c.RequireSentContains {
		found := false
		for _, got := range ev.Sent {
			if strings.Contains(got, s) {
				found = true
				break
			}
		}
		if !found {
			return fmt.Sprintf("%s: PASS without adversarial input %q in sent corpus", p.Name, s)
		}
	}
	for _, s := range c.RequireAnyContains {
		if !ev.anyContains(s) {
			return fmt.Sprintf("%s: PASS without %q in request/response evidence", p.Name, s)
		}
	}
	if len(c.RequireStatus) > 0 {
		ok := false
		for _, st := range ev.statuses() {
			if statusAllowed(st, c.RequireStatus) {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Sprintf("%s: PASS without expected status %v (got %v)", p.Name, c.RequireStatus, ev.statuses())
		}
	}
	return ""
}
