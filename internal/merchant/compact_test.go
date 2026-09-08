package merchant

import "testing"

func TestParseCompactConfig(t *testing.T) {
	p, err := Parse([]byte(`
merchant: example
identity:
  source: jwt
  claim: customer_id
concurrency:
  max_active: 1
lease:
  ttl: 60s
routes:
  - match: { method: POST, path: "/api/holds" }
    resource_from: event_id
    resource_prefix: "event:"
    action: hold
`))
	if err != nil {
		t.Fatal(err)
	}
	if p.MerchantID != "example" {
		t.Fatalf("merchant=%s", p.MerchantID)
	}
	if p.Identity.Extractor != "bearer-jwt" {
		t.Fatalf("extractor=%s", p.Identity.Extractor)
	}
	if p.Identity.SubjectClaim != "customer_id" {
		t.Fatalf("claim=%s", p.Identity.SubjectClaim)
	}
	if p.Policy.MaxActive != 1 || p.Policy.LeaseTTL != "60s" {
		t.Fatalf("policy %+v", p.Policy)
	}
}
