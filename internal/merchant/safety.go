package merchant

import (
	"fmt"
	"strings"

	"github.com/Nareik33L/bruiser-gateway/internal/policy"
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
	JWKSURL      string
	Issuer       string
	Audience     string
}

// FailOpenReasons lists profile-level fail-open behaviour: unmatched
// allow and an allow-uncontrolled policy fallback. Discovery routes
// with control: none are not fail-open.
func (p Profile) FailOpenReasons() []string {
	var reasons []string
	if p.UnmatchedAllow() {
		reasons = append(reasons, "unmatched: allow")
	}
	reasons = append(reasons, policy.FailOpenReasons(p.PolicyDocument())...)
	return reasons
}

// ValidateProductionPolicy rejects a fail-open profile in production
// unless BRUISER_ALLOW_UNSAFE_MODES acknowledged the risk.
func ValidateProductionPolicy(p Profile, allowUnsafe bool) error {
	reasons := p.FailOpenReasons()
	if len(reasons) == 0 {
		return nil
	}
	if allowUnsafe {
		return nil
	}
	return fmt.Errorf("production policy is fail-open (%s); set BRUISER_ALLOW_UNSAFE_MODES=1 to acknowledge", strings.Join(reasons, ", "))
}

// SafetyIssues is the unmatched:allow / origin-lockdown boundary.
// unmatched: deny is the shipped default. unmatched: allow is fail-open
// for unlisted routes; production cannot combine it with a missing
// origin lockdown, and ValidateProductionPolicy additionally requires
// BRUISER_ALLOW_UNSAFE_MODES.
func (p Profile) SafetyIssues(opts SafetyOpts) []Issue {
	var out []Issue
	if opts.Production {
		ext := strings.ToLower(strings.TrimSpace(p.Identity.Extractor))
		switch ext {
		case "oidc", "jwks", "auto", "cookie-jwt", "cookie", "bearer-jwt", "bearer", "jwt":
			if p.Identity.JWKSURL == "" && opts.JWKSURL == "" {
				out = append(out, Issue{"FAIL", "identity.jwks_url", "production requires jwks_url (HS256 is development-only)"})
			}
			if p.Identity.Issuer == "" && opts.Issuer == "" {
				out = append(out, Issue{"FAIL", "identity.issuer", "production requires identity.issuer"})
			}
			if p.Identity.Audience == "" && opts.Audience == "" {
				out = append(out, Issue{"FAIL", "identity.audience", "production requires identity.audience"})
			}
			if ext == "auto" && strings.TrimSpace(p.Identity.Header) != "" && !p.Identity.AllowUnsignedHeader {
				out = append(out, Issue{"FAIL", "identity.header", "production refuses extractor: auto with an unsigned customer header unless identity.allow_unsigned_header is explicitly true"})
			}
		case "header":
			if !p.Identity.AllowUnsignedHeader {
				out = append(out, Issue{"FAIL", "identity.extractor", "production refuses unsigned header identity unless identity.allow_unsigned_header is explicitly true"})
			}
		}
	}
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
