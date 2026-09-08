package main

import (
	"fmt"
	"os"

	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
	"github.com/Nareik33L/bruiser-gateway/internal/policy"
)

func cmdProfile() error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: bruiser profile validate <file.yaml> | bruiser profile init")
	}
	switch os.Args[2] {
	case "validate":
		path := "configs/example.yaml"
		if len(os.Args) > 3 {
			path = os.Args[3]
		}
		p, err := merchant.LoadFile(path)
		if err != nil {
			return err
		}
		doc := p.PolicyDocument()
		if _, err := policy.Compile(doc); err != nil {
			return err
		}
		fails := 0
		for _, i := range p.ValidateIssues() {
			fmt.Printf("%s  %s: %s\n", i.Level, i.Field, i.Message)
			if i.Level == "FAIL" {
				fails++
			}
		}
		n := 0
		for _, r := range p.Routes {
			if r.Controlled() {
				n++
			}
		}
		if fails > 0 {
			return fmt.Errorf("profile validate: %d error(s)", fails)
		}
		fmt.Printf("ok merchant=%s extractor=%s unmatched=%s allocation_routes=%d\n",
			p.MerchantID, p.Identity.Extractor, p.Unmatched, n)
		return nil
	case "init":
		fmt.Print(starterProfile)
		return nil
	default:
		return fmt.Errorf("usage: bruiser profile validate <file.yaml> | bruiser profile init")
	}
}

const starterProfile = `# Bruiser drop-in profile.
# 1. List allocation routes (hold/purchase). Everything else passes through.
# 2. Point Edge (auth_request / ext_authz) or Proxy (BRUISER_PROXY_ADDR + ORIGIN_URL).
# 3. Lock origin so allocation rejects traffic that skipped Bruiser.
# 4. bruiser authority-check --front <edge-or-proxy> --origin <box-office>

merchant_id: my-merchant
unmatched: allow

identity:
  extractor: auto
  cookie: session
  header: X-Customer-Id
  subject_claim: sub
  hmac_secret_env: BRUISER_DEV_HMAC_SECRET

routes:
  - match: { method: POST, path: "/api/holds" }
    resource_from: event_id
    resource_prefix: "sku:"
    action: hold
  - match: { method: POST, path: "/api/orders" }
    resource_from: event_id
    resource_prefix: "sku:"
    action: purchase

policy:
  version: 1
  defaults: { lease_ttl: 60s, max_lifetime: 15m }
  domains:
    - name: one-execution-per-customer
      match: { action: [purchase, hold] }
      scope: [customer, resource]
      max_active: 1
      precedence: [browser, agent]
      budget: { max_ops: 1 }
      # waiting: { mode: bounded, max_waiters: 1 }
  fallback: deny
`
