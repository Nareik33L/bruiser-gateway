package merchant

import (
	"strings"
)

// CommonAllocationPaths are hold/purchase aliases clubs often forget to list.
// Authority Check probes these on the front and the origin. Config validate
// warns when unmatched: allow and a path is missing from the profile.
var CommonAllocationPaths = []string{
	"/api/tickets/hold",
	"/api/tickets/holds",
	"/api/ticket/hold",
	"/api/holds",
	"/api/hold",
	"/boxoffice/holds",
	"/v1/holds",
	"/api/orders",
	"/api/checkout",
}

type SafetyOpts struct {
	OriginSecret string
	OriginURL    string
	Proxy        bool
	Production   bool
}

// SafetyIssues is the unmatched:allow / origin-lockdown boundary.
// unmatched: allow stays the default (discovery fail-open) but production
// cannot combine it with a missing origin lockdown.
func (p Profile) SafetyIssues(opts SafetyOpts) []Issue {
	var out []Issue
	if !p.UnmatchedAllow() {
		return out
	}
	if strings.TrimSpace(opts.OriginSecret) == "" {
		out = append(out, Issue{
			Level:   "FAIL",
			Field:   "unmatched",
			Message: "unmatched: allow without BRUISER_ORIGIN_SECRET — unlisted hold/purchase paths can reach origin. Enable origin lockdown or set unmatched: deny",
		})
	} else {
		out = append(out, Issue{
			Level:   "WARN",
			Field:   "unmatched",
			Message: "unmatched: allow is fail-open for unlisted routes. Every scarce hold/purchase path must be listed. Origin lockdown is required. Go-live: bruiser authority-check --front <edge> --origin <origin> --control <bruiser>",
		})
	}
	if opts.Production && opts.Proxy && strings.TrimSpace(opts.OriginURL) == "" {
		out = append(out, Issue{
			Level:   "FAIL",
			Field:   "origin_url",
			Message: "production Proxy requires BRUISER_ORIGIN_URL so authority-check can prove origin lockdown",
		})
	}
	missing := p.unlistedCommonAllocation()
	if len(missing) > 0 {
		out = append(out, Issue{
			Level:   "WARN",
			Field:   "routes",
			Message: "unmatched: allow — common allocation paths not in the profile: " + strings.Join(missing, ", ") + ". If the origin serves any of these, they bypass Bruiser unless origin lockdown rejects them",
		})
	}
	return out
}

func (p Profile) unlistedCommonAllocation() []string {
	var missing []string
	for _, path := range CommonAllocationPaths {
		if !p.hasMethodPath("POST", path) {
			missing = append(missing, path)
		}
	}
	return missing
}

func (p Profile) hasMethodPath(method, path string) bool {
	_, _, ok := p.MatchRoute(method, path)
	return ok
}

// LooksLikeAllocation is true for routes that smell like scarce inventory
// but were not given a resource (so unmatched: allow would pass them through).
func LooksLikeAllocation(r Route) bool {
	if r.Controlled() {
		return false
	}
	blob := strings.ToLower(r.Action + " " + r.Match.Path)
	for _, n := range []string{"hold", "purchase", "allocate", "checkout", "order", "ticket", "reserve"} {
		if strings.Contains(blob, n) {
			return true
		}
	}
	return false
}
