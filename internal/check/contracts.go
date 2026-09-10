package check

// Contract is the meta-rule for a named probe: a PASS is invalid unless
// the recorded evidence contains the adversarial input the probe claims.
type Contract struct {
	Name                 string
	RequireHeaderPresent []string
	RequireSentContains  []string
	RequireAnyContains   []string
	RequireStatus        []int
	MinExchanges         int
}

// Contracts is the authoritative list of probes that must not PASS on a
// lockdown side-effect. Every named probe in this list is asserted by
// TestNoPassWithoutAdversarialInput.
func Contracts() []Contract {
	return []Contract{
		{
			Name:                 "Forged execution rejected at origin",
			RequireHeaderPresent: []string{"X-Bruiser-Origin-Secret"},
			RequireAnyContains:   []string{"forged.not.signed"},
			RequireStatus:        []int{403},
			MinExchanges:         1,
		},
		{
			Name:                 "Stale fence rejected at origin",
			RequireHeaderPresent: []string{"X-Bruiser-Origin-Secret"},
			RequireSentContains:  []string{"stale-fence-token"},
			RequireStatus:        []int{403},
			MinExchanges:         1,
		},
		{
			Name:                "Resource variants share one domain",
			RequireSentContains: []string{"ticket:cupfinal", "ticket:cupfinal.", "ticket:cupfinal\u2010", "ticket:cupfinal\u2013"},
			MinExchanges:        9,
		},
		{
			Name:               "Identity forgery rejected (dev assertions off)",
			RequireAnyContains: []string{"not-a-jwt"},
			MinExchanges:       1,
		},
		{
			Name:                "Session revocation takes effect",
			RequireSentContains: []string{"revoked-session"},
			MinExchanges:        1,
		},
		{
			Name:               "Replay rejected",
			RequireAnyContains: []string{"authority-check-nonce"},
			RequireStatus:      []int{409},
			MinExchanges:       2,
		},
		{
			Name:                "Budget enforced",
			RequireSentContains: []string{"budget"},
			MinExchanges:        2,
		},
		{
			Name:               "Admin surface not on public listener",
			RequireAnyContains: []string{"/v1/admin/status"},
			MinExchanges:       1,
		},
		{
			Name:                "Fail-closed on store outage",
			RequireSentContains: []string{"store-down"},
			MinExchanges:        1,
		},
		{
			Name:                "Production secret validation",
			RequireSentContains: []string{"validate-secrets"},
			MinExchanges:        1,
		},
	}
}

func statusAllowed(got int, allow []int) bool {
	if len(allow) == 0 {
		return true
	}
	for _, a := range allow {
		if got == a {
			return true
		}
	}
	return false
}
