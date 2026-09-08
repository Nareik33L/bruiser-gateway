package merchant

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/policy"
)

// Issue is an actionable configuration problem on a merchant profile.
type Issue struct {
	Level   string // FAIL or WARN
	Field   string
	Message string
}

func (i Issue) String() string {
	return i.Level + " " + i.Field + ": " + i.Message
}

var knownExtractors = map[string]bool{
	"auto": true, "cookie-jwt": true, "cookie": true, "bearer-jwt": true,
	"bearer": true, "jwt": true, "header": true, "introspect": true,
	"introspection": true, "edge-signed": true, "signed-header": true,
	"oidc": true, "jwks": true,
}

// ValidateIssues returns FAIL/WARN findings. It does not change Parse.
func (p Profile) ValidateIssues() []Issue {
	var out []Issue
	if p.MerchantID == "" {
		out = append(out, Issue{"FAIL", "merchant_id", "merchant_id is required"})
	}
	ext := strings.ToLower(strings.TrimSpace(p.Identity.Extractor))
	if ext == "" {
		out = append(out, Issue{"FAIL", "identity.extractor", "missing identity configuration (extractor)"})
	} else if !knownExtractors[ext] {
		out = append(out, Issue{"FAIL", "identity.extractor", "unknown extractor " + p.Identity.Extractor + " (auto|cookie-jwt|bearer-jwt|header|introspect|edge-signed|oidc)"})
	}
	switch ext {
	case "oidc", "jwks":
		if p.Identity.JWKSURL == "" {
			out = append(out, Issue{"FAIL", "identity.jwks_url", "oidc extractor requires jwks_url"})
		}
	case "introspect", "introspection":
		if p.Identity.IntrospectURL == "" {
			out = append(out, Issue{"FAIL", "identity.introspect_url", "introspect extractor requires introspect_url"})
		}
	case "cookie-jwt", "cookie":
		if p.Identity.Cookie == "" {
			out = append(out, Issue{"FAIL", "identity.cookie", "cookie-jwt extractor requires cookie name"})
		}
	}
	if p.Identity.SubjectClaim == "" && (ext == "cookie-jwt" || ext == "bearer-jwt" || ext == "auto" || ext == "oidc") {
		out = append(out, Issue{"WARN", "identity.subject_claim", "subject claim empty; defaulting to sub"})
	}
	if env := p.Identity.HMACSecretEnv; env != "" {
		if os.Getenv(env) == "" {
			out = append(out, Issue{"WARN", "identity.hmac_secret_env", env + " is not set in this environment"})
		}
	}
	if p.Mode != "" && !strings.EqualFold(p.Mode, "enforce") && !strings.EqualFold(p.Mode, "dry-run") {
		out = append(out, Issue{"FAIL", "mode", "mode must be enforce or dry-run"})
	}
	n := 0
	for _, r := range p.Routes {
		if r.Match.Method == "" || r.Match.Path == "" {
			out = append(out, Issue{"FAIL", "routes", "route missing method or path"})
		}
		if r.Controlled() {
			n++
		}
		if LooksLikeAllocation(r) && !r.Controlled() {
			out = append(out, Issue{"FAIL", "routes", r.Match.Method + " " + r.Match.Path + " looks like allocation (action=" + r.Action + ") but has no resource/resource_from — unmatched traffic would pass it through"})
		}
	}
	if n == 0 {
		out = append(out, Issue{"FAIL", "routes", "no protected allocation routes (set resource or resource_from on hold/purchase paths)"})
	}
	if strings.EqualFold(p.Unmatched, "deny") {
		out = append(out, Issue{"WARN", "unmatched", "unmatched deny fails closed on discovery traffic; default is allow"})
	}
	if p.Policy.Waiting.Mode != "" && !strings.EqualFold(p.Policy.Waiting.Mode, "bounded") && !strings.EqualFold(p.Policy.Waiting.Mode, "off") {
		out = append(out, Issue{"FAIL", "policy.waiting.mode", "waiting.mode must be bounded or off"})
	}
	if strings.EqualFold(p.Policy.Waiting.Mode, "bounded") && p.Policy.Waiting.MaxWaiters < 1 {
		out = append(out, Issue{"FAIL", "policy.waiting.max_waiters", "bounded queue requires max_waiters >= 1"})
	}
	if p.Policy.LeaseTTL != "" {
		if d, err := time.ParseDuration(p.Policy.LeaseTTL); err != nil {
			out = append(out, Issue{"FAIL", "policy.lease_ttl", "invalid lease ttl: " + err.Error()})
		} else if d < time.Second {
			out = append(out, Issue{"WARN", "policy.lease_ttl", "lease ttl under 1s is unusually short"})
		}
	}
	if p.Policy.MaxLifetime != "" && p.Policy.LeaseTTL != "" {
		life, e1 := time.ParseDuration(p.Policy.MaxLifetime)
		ttl, e2 := time.ParseDuration(p.Policy.LeaseTTL)
		if e1 == nil && e2 == nil && life < ttl {
			out = append(out, Issue{"FAIL", "policy.max_lifetime", "max_lifetime must be >= lease_ttl"})
		}
	}
	if _, err := policy.Compile(p.PolicyDocument()); err != nil {
		out = append(out, Issue{"FAIL", "policy", err.Error()})
	}
	return out
}

func (p Profile) Validate() error {
	for _, i := range p.ValidateIssues() {
		if i.Level == "FAIL" {
			return fmt.Errorf("%s", i.String())
		}
	}
	return nil
}
