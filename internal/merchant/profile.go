package merchant

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Profile struct {
	MerchantID  string `yaml:"merchant_id"`
	Name        string `yaml:"name"`
	ProfileName string `yaml:"profile"`
	Assumptions string `yaml:"assumptions"`
	// Unmatched is "allow" (default) or "deny". Allocation routes fail closed;
	// everything else fails open so Bruiser can sit in front of a whole origin.
	Unmatched string   `yaml:"unmatched"`
	Mode      string   `yaml:"mode"` // enforce (default) or dry-run
	Identity  Identity `yaml:"identity"`
	Routes    []Route  `yaml:"routes"`
	Policy    Policy   `yaml:"policy"`
}

type Identity struct {
	Extractor       string `yaml:"extractor"` // auto | cookie-jwt | bearer-jwt | header | introspect | edge-signed | oidc
	Source          string `yaml:"source"`    // compact alias of extractor
	Cookie          string `yaml:"cookie"`
	Header          string `yaml:"header"`
	PrincipalHeader string `yaml:"principal_header"`
	SubjectClaim    string `yaml:"subject_claim"`
	Claim           string `yaml:"claim"` // compact alias of subject_claim
	HMACSecretEnv   string `yaml:"hmac_secret_env"`
	IntrospectURL   string `yaml:"introspect_url"`
	JWKSURL         string `yaml:"jwks_url"`
	Issuer          string `yaml:"issuer"`
	Audience        string `yaml:"audience"`
}

// Compact fields — accepted at the document root so a merchant can write
// merchant / identity.source / concurrency.max_active / lease.ttl.
type compactRoot struct {
	Merchant    string `yaml:"merchant"`
	Concurrency struct {
		MaxActive int `yaml:"max_active"`
	} `yaml:"concurrency"`
	Resource struct {
		Scope string `yaml:"scope"`
	} `yaml:"resource"`
	Lease struct {
		TTL       string `yaml:"ttl"`
		Heartbeat string `yaml:"heartbeat"`
	} `yaml:"lease"`
	Waiting struct {
		Mode       string `yaml:"mode"`
		MaxWaiters int    `yaml:"max_waiters"`
	} `yaml:"waiting"`
}

type Route struct {
	Match          Match  `yaml:"match"`
	Action         string `yaml:"action"`
	Resource       string `yaml:"resource"`
	ResourceFrom   string `yaml:"resource_from"`
	ResourcePrefix string `yaml:"resource_prefix"`
}

type Match struct {
	Method string `yaml:"method"`
	Path   string `yaml:"path"`
}

func LoadFile(path string) (Profile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, err
	}
	return Parse(b)
}

func Parse(b []byte) (Profile, error) {
	var p Profile
	if err := yaml.Unmarshal(b, &p); err != nil {
		return p, err
	}
	var compact compactRoot
	_ = yaml.Unmarshal(b, &compact)
	if p.MerchantID == "" {
		p.MerchantID = compact.Merchant
	}
	if p.Identity.Extractor == "" && p.Identity.Source != "" {
		p.Identity.Extractor = mapSource(p.Identity.Source)
	}
	if p.Identity.SubjectClaim == "" && p.Identity.Claim != "" {
		p.Identity.SubjectClaim = p.Identity.Claim
	}
	if compact.Concurrency.MaxActive > 0 && p.Policy.MaxActive < 1 {
		p.Policy.MaxActive = compact.Concurrency.MaxActive
	}
	if compact.Lease.TTL != "" && p.Policy.LeaseTTL == "" {
		p.Policy.LeaseTTL = compact.Lease.TTL
	}
	if compact.Waiting.Mode != "" || compact.Waiting.MaxWaiters > 0 {
		p.Policy.Waiting.Mode = compact.Waiting.Mode
		p.Policy.Waiting.MaxWaiters = compact.Waiting.MaxWaiters
	}
	if p.MerchantID == "" {
		return p, fmt.Errorf("merchant_id required")
	}
	if p.Policy.MaxActive < 1 {
		p.Policy.MaxActive = 1
	}
	if p.Policy.RuleName == "" {
		p.Policy.RuleName = "purchase-per-event"
	}
	if p.Identity.SubjectClaim == "" {
		p.Identity.SubjectClaim = "sub"
	}
	if p.Identity.Extractor == "" {
		p.Identity.Extractor = "auto"
	}
	if p.Identity.Header == "" {
		p.Identity.Header = "X-Customer-Id"
	}
	if p.Unmatched == "" {
		p.Unmatched = "allow"
	}
	return p, nil
}

func mapSource(src string) string {
	switch strings.ToLower(strings.TrimSpace(src)) {
	case "jwt", "bearer":
		return "bearer-jwt"
	case "cookie", "cookie-jwt":
		return "cookie-jwt"
	case "header":
		return "header"
	case "introspect", "introspection":
		return "introspect"
	case "edge-signed", "signed-header":
		return "edge-signed"
	case "oidc", "jwks":
		return "oidc"
	default:
		return src
	}
}

func (p Profile) UnmatchedAllow() bool {
	return !strings.EqualFold(p.Unmatched, "deny")
}

// MatchRoute returns the first matching route and path params.
func (p Profile) MatchRoute(method, path string) (Route, map[string]string, bool) {
	method = strings.ToUpper(method)
	for _, r := range p.Routes {
		if strings.ToUpper(r.Match.Method) != method {
			continue
		}
		params, ok := matchPath(r.Match.Path, path)
		if ok {
			return r, params, true
		}
	}
	return Route{}, nil, false
}

func matchPath(pattern, path string) (map[string]string, bool) {
	pattern = strings.TrimSuffix(pattern, "/")
	path = strings.TrimSuffix(path, "/")
	pp := strings.Split(pattern, "/")
	sp := strings.Split(path, "/")
	if len(pp) != len(sp) {
		return nil, false
	}
	params := map[string]string{}
	for i := range pp {
		if strings.HasPrefix(pp[i], "{") && strings.HasSuffix(pp[i], "}") {
			params[strings.TrimSuffix(strings.TrimPrefix(pp[i], "{"), "}")] = sp[i]
			continue
		}
		if pp[i] != sp[i] {
			return nil, false
		}
	}
	return params, true
}

func (r Route) ResourceFor(params map[string]string, bodyEventID string) string {
	if r.ResourceFrom == "event_id" && bodyEventID != "" {
		return r.ResourcePrefix + bodyEventID
	}
	res := r.Resource
	for k, v := range params {
		res = strings.ReplaceAll(res, "{"+k+"}", v)
	}
	return res
}

// Controlled is true when the route maps onto a scarcity action Bruiser must admit.
func (r Route) Controlled() bool {
	return r.Resource != "" || r.ResourceFrom != ""
}

func Empty(merchantID string) Profile {
	p, err := Parse([]byte("merchant_id: " + merchantID + "\n"))
	if err != nil {
		panic(err)
	}
	return p
}
