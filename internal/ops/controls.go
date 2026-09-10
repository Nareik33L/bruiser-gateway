// Package ops is runtime operator state: enforcement mode and emergency
// controls. It does not change the product: one customer remains one customer.
package ops

import "strings"

const (
	ModeEnforce = "enforce"
	ModeDryRun  = "dry-run"
)

type Controls struct {
	Mode            string    `json:"mode"`
	Enforcement     bool      `json:"enforcement"`
	EnforcePercent  int       `json:"enforce_percent"`
	RampSalt        string    `json:"ramp_salt,omitempty"`
	Scope           RampScope `json:"scope"`
	QueueEnabled    bool      `json:"queue_enabled"`
	MaxWaiters      int       `json:"max_waiters"`
	LeaseTTLSeconds int       `json:"lease_ttl_seconds"`
	FailClosed      bool      `json:"fail_closed"`
	DisabledActions []string  `json:"disabled_actions,omitempty"`
	UpdatedBy       string    `json:"updated_by,omitempty"`
}

func Default() Controls {
	return Controls{
		Mode:           ModeEnforce,
		Enforcement:    true,
		EnforcePercent: 100,
		QueueEnabled:   true,
		FailClosed:     true,
	}
}

func FromEnv(mode string, enforcement, queue bool, percent int) Controls {
	c := Default()
	if stringsEqual(mode, ModeDryRun) {
		c.Mode = ModeDryRun
		c.EnforcePercent = 0
	}
	if percent >= 0 {
		c.EnforcePercent = ClampPercent(percent)
	}
	c.Enforcement = enforcement
	c.QueueEnabled = queue
	return c
}

func (c Controls) DryRun() bool {
	return stringsEqual(c.Mode, ModeDryRun) || c.EffectivePercent() == 0
}

func (c Controls) PassThrough() bool {
	return !c.Enforcement
}

// EffectivePercent is the active-enforcement share. Observation is always 100%.
// Dry-run mode or the enforcement kill switch force 0% without dropping evaluate/record.
func (c Controls) EffectivePercent() int {
	if !c.Enforcement || stringsEqual(c.Mode, ModeDryRun) {
		return 0
	}
	return ClampPercent(c.EnforcePercent)
}

func (c Controls) ShouldEnforce(in RampInput) bool {
	p := c.EffectivePercent()
	if p <= 0 {
		return false
	}
	if !c.Scope.Match(in) {
		return false
	}
	return InRamp(in.CustomerID, p, c.RampSalt)
}

func (c Controls) EffectiveWaiters(policyWaiters int) int {
	if !c.QueueEnabled {
		return 0
	}
	if c.MaxWaiters > 0 {
		return c.MaxWaiters
	}
	return policyWaiters
}

func (c Controls) ActionDisabled(action string) bool {
	for _, a := range c.DisabledActions {
		if stringsEqual(a, action) {
			return true
		}
	}
	return false
}

func Normalize(c Controls) Controls {
	if c.Mode != ModeDryRun {
		c.Mode = ModeEnforce
	}
	c.EnforcePercent = ClampPercent(c.EnforcePercent)
	if c.Mode == ModeDryRun {
		c.EnforcePercent = 0
	}
	var acts []string
	for _, a := range c.DisabledActions {
		a = strings.TrimSpace(a)
		if a != "" {
			acts = append(acts, a)
		}
	}
	c.DisabledActions = acts
	return c
}

func JoinActions(acts []string) string {
	return strings.Join(acts, ",")
}

func SplitActions(raw string) []string {
	if raw == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func stringsEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if lower(a[i]) != lower(b[i]) {
			return false
		}
	}
	return true
}

func lower(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + 32
	}
	return b
}
