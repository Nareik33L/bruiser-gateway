package merchant

import "github.com/Nareik33L/bruiser-gateway/internal/policy"

type Policy struct {
	Version     int             `yaml:"version"`
	Defaults    policy.Defaults `yaml:"defaults"`
	Domains     []policy.Rule   `yaml:"domains"`
	Fallback    string          `yaml:"fallback"`
	RuleName    string          `yaml:"rule_name"`
	MaxActive   int             `yaml:"max_active"`
	LeaseTTL    string          `yaml:"lease_ttl"`
	MaxLifetime string          `yaml:"max_lifetime"`
	Waiting     policy.Waiting  `yaml:"waiting"`
}

func (p Profile) PolicyDocument() policy.Document {
	if len(p.Policy.Domains) > 0 {
		d := policy.Document{
			Version:  p.Policy.Version,
			Merchant: p.MerchantID,
			Defaults: p.Policy.Defaults,
			Domains:  p.Policy.Domains,
			Fallback: p.Policy.Fallback,
		}
		if d.Version == 0 {
			d.Version = 1
		}
		return d
	}
	doc := policy.DefaultDocument(p.MerchantID)
	if p.Policy.RuleName != "" {
		doc.Domains[0].Name = p.Policy.RuleName
	}
	if p.Policy.MaxActive > 0 {
		doc.Domains[0].MaxActive = p.Policy.MaxActive
	}
	if p.Policy.LeaseTTL != "" {
		doc.Domains[0].LeaseTTL = p.Policy.LeaseTTL
		doc.Defaults.LeaseTTL = p.Policy.LeaseTTL
	}
	if p.Policy.MaxLifetime != "" {
		doc.Domains[0].MaxLifetime = p.Policy.MaxLifetime
		doc.Defaults.MaxLifetime = p.Policy.MaxLifetime
	}
	if p.Policy.Waiting.Mode != "" || p.Policy.Waiting.MaxWaiters > 0 {
		doc.Domains[0].Waiting = p.Policy.Waiting
	}
	return doc
}
