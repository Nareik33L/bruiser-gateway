package policy

import (
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/Nareik33L/bruiser-gateway/internal/lease"
)

const (
	FallbackDeny              = "deny"
	FallbackAllowUncontrolled = "allow-uncontrolled"
	MissingDeny               = "deny"
	MissingFallthrough        = "fallthrough"
	ControlNone               = "none"
)

type Document struct {
	Version  int      `yaml:"version" json:"version"`
	Merchant string   `yaml:"merchant" json:"merchant"`
	Defaults Defaults `yaml:"defaults" json:"defaults"`
	Domains  []Rule   `yaml:"domains" json:"domains"`
	Fallback string   `yaml:"fallback" json:"fallback"`
}

type Defaults struct {
	LeaseTTL       string   `yaml:"lease_ttl" json:"lease_ttl"`
	MaxLifetime    string   `yaml:"max_lifetime" json:"max_lifetime"`
	SessionTTL     string   `yaml:"session_ttl" json:"session_ttl"`
	PrincipalTypes []string `yaml:"principal_types" json:"principal_types"`
}

type Rule struct {
	Name            string   `yaml:"name" json:"name"`
	Match           Match    `yaml:"match" json:"match"`
	Scope           []string `yaml:"scope" json:"scope"`
	MaxActive       int      `yaml:"max_active" json:"max_active"`
	LeaseTTL        string   `yaml:"lease_ttl" json:"lease_ttl"`
	MaxLifetime     string   `yaml:"max_lifetime" json:"max_lifetime"`
	Precedence      []string `yaml:"precedence" json:"precedence"`
	Control         string   `yaml:"control" json:"control"`
	OnMissingAnchor string   `yaml:"on_missing_anchor" json:"on_missing_anchor"`
}

type Match struct {
	Action         []string `yaml:"action" json:"action"`
	ResourcePrefix string   `yaml:"resource_prefix" json:"resource_prefix"`
}

type Request struct {
	MerchantID    string
	CustomerID    string
	PrincipalType string
	Resource      string
	Action        string
	Anchors       map[string]string
}

type Decision struct {
	RuleName    string
	DomainKey   string
	MaxActive   int
	TTL         time.Duration
	MaxLifetime time.Duration
	Precedence  []string
	ControlNone bool
	Denied      bool
	Reason      string
}

type compiledRule struct {
	Rule
	ttl      time.Duration
	lifetime time.Duration
}

type Compiled struct {
	Doc        Document
	defaults   Defaults
	ttl        time.Duration
	lifetime   time.Duration
	rules      []compiledRule
	fallback   string
	precedence []string
}

func Parse(raw []byte) (Document, error) {
	var d Document
	if err := yaml.Unmarshal(raw, &d); err != nil {
		return d, fmt.Errorf("policy yaml: %w", err)
	}
	return d, nil
}

func CompileYAML(raw []byte) (Compiled, error) {
	d, err := Parse(raw)
	if err != nil {
		return Compiled{}, err
	}
	return Compile(d)
}

func Compile(d Document) (Compiled, error) {
	if d.Version == 0 {
		d.Version = 1
	}
	if d.Version != 1 {
		return Compiled{}, fmt.Errorf("unsupported policy version %d", d.Version)
	}
	if d.Fallback == "" {
		d.Fallback = FallbackDeny
	}
	if d.Fallback != FallbackDeny && d.Fallback != FallbackAllowUncontrolled {
		return Compiled{}, fmt.Errorf("fallback must be deny or allow-uncontrolled")
	}
	defTTL, err := parseDur(d.Defaults.LeaseTTL, 60*time.Second)
	if err != nil {
		return Compiled{}, fmt.Errorf("defaults.lease_ttl: %w", err)
	}
	defLife, err := parseDur(d.Defaults.MaxLifetime, 15*time.Minute)
	if err != nil {
		return Compiled{}, fmt.Errorf("defaults.max_lifetime: %w", err)
	}
	defPrec := []string{"browser", "agent"}
	if len(d.Domains) == 0 {
		return Compiled{}, fmt.Errorf("policy has no domains")
	}
	out := Compiled{
		Doc: d, ttl: defTTL, lifetime: defLife, fallback: d.Fallback, precedence: defPrec,
	}
	for i, r := range d.Domains {
		if r.Name == "" {
			return Compiled{}, fmt.Errorf("domains[%d]: name required", i)
		}
		if r.Control == ControlNone {
			out.rules = append(out.rules, compiledRule{Rule: r})
			continue
		}
		if len(r.Scope) == 0 {
			return Compiled{}, fmt.Errorf("rule %s: scope required", r.Name)
		}
		for _, dim := range r.Scope {
			if !validDim(dim) {
				return Compiled{}, fmt.Errorf("rule %s: unknown scope dimension %q", r.Name, dim)
			}
		}
		if r.MaxActive < 1 {
			r.MaxActive = 1
		}
		if r.OnMissingAnchor == "" {
			r.OnMissingAnchor = MissingDeny
		}
		if r.OnMissingAnchor != MissingDeny && r.OnMissingAnchor != MissingFallthrough {
			return Compiled{}, fmt.Errorf("rule %s: on_missing_anchor", r.Name)
		}
		ttl, err := parseDur(r.LeaseTTL, defTTL)
		if err != nil {
			return Compiled{}, fmt.Errorf("rule %s lease_ttl: %w", r.Name, err)
		}
		life, err := parseDur(r.MaxLifetime, defLife)
		if err != nil {
			return Compiled{}, fmt.Errorf("rule %s max_lifetime: %w", r.Name, err)
		}
		if len(r.Precedence) == 0 {
			r.Precedence = append([]string{}, defPrec...)
		}
		out.rules = append(out.rules, compiledRule{Rule: r, ttl: ttl, lifetime: life})
	}
	return out, nil
}

func (c Compiled) Evaluate(req Request) Decision {
	for _, r := range c.rules {
		if !matchRule(r.Rule, req) {
			continue
		}
		if r.Control == ControlNone {
			return Decision{RuleName: r.Name, ControlNone: true}
		}
		key, err := domainKey(req, r.Name, r.Scope)
		if err != nil {
			if r.OnMissingAnchor == MissingFallthrough {
				continue
			}
			return Decision{RuleName: r.Name, Denied: true, Reason: "missing_anchor"}
		}
		return Decision{
			RuleName:    r.Name,
			DomainKey:   key,
			MaxActive:   r.MaxActive,
			TTL:         r.ttl,
			MaxLifetime: r.lifetime,
			Precedence:  r.Precedence,
		}
	}
	if c.fallback == FallbackAllowUncontrolled {
		return Decision{RuleName: "fallback", ControlNone: true}
	}
	return Decision{RuleName: "fallback", Denied: true, Reason: "no_matching_rule"}
}

func (c Compiled) Precedence(ruleName string) []string {
	for _, r := range c.rules {
		if r.Name == ruleName && len(r.Precedence) > 0 {
			return r.Precedence
		}
	}
	return c.precedence
}

func matchRule(r Rule, req Request) bool {
	if len(r.Match.Action) > 0 {
		ok := false
		for _, a := range r.Match.Action {
			if a == req.Action {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if r.Match.ResourcePrefix != "" && !strings.HasPrefix(req.Resource, r.Match.ResourcePrefix) {
		return false
	}
	return true
}

func domainKey(req Request, ruleName string, scope []string) (string, error) {
	parts := []string{req.MerchantID, ruleName}
	for _, dim := range scope {
		val, err := dimValue(req, dim)
		if err != nil {
			return "", err
		}
		parts = append(parts, dim+"="+val)
	}
	return strings.Join(parts, "/"), nil
}

func dimValue(req Request, dim string) (string, error) {
	switch {
	case dim == "customer":
		return req.CustomerID, nil
	case dim == "resource":
		return req.Resource, nil
	case dim == "resource_pool":
		if i := strings.LastIndex(req.Resource, ":"); i > 0 {
			return req.Resource[:i], nil
		}
		return req.Resource, nil
	case dim == "principal_type":
		return req.PrincipalType, nil
	case strings.HasPrefix(dim, "anchor:"):
		name := strings.TrimPrefix(dim, "anchor:")
		if req.Anchors == nil || req.Anchors[name] == "" {
			return "", fmt.Errorf("missing_anchor")
		}
		return req.Anchors[name], nil
	default:
		return "", fmt.Errorf("unknown dimension")
	}
}

func validDim(dim string) bool {
	switch dim {
	case "customer", "resource", "resource_pool", "principal_type":
		return true
	}
	return strings.HasPrefix(dim, "anchor:") && len(dim) > len("anchor:")
}

func parseDur(s string, def time.Duration) (time.Duration, error) {
	if s == "" {
		return def, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d <= 0 {
		return 0, fmt.Errorf("must be > 0")
	}
	return d, nil
}

func DefaultDocument(merchantID string) Document {
	return Document{
		Version:  1,
		Merchant: merchantID,
		Defaults: Defaults{LeaseTTL: "60s", MaxLifetime: "15m"},
		Domains: []Rule{{
			Name:        lease.DefaultRuleName(),
			Match:       Match{Action: []string{"purchase", "hold"}},
			Scope:       []string{"customer", "resource"},
			MaxActive:   1,
			LeaseTTL:    "60s",
			MaxLifetime: "15m",
			Precedence:  []string{"browser", "agent"},
		}, {
			Name:    "discovery",
			Match:   Match{Action: []string{"search"}},
			Control: ControlNone,
		}},
		Fallback: FallbackDeny,
	}
}
