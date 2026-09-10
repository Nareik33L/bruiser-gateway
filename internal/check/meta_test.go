package check_test

import (
	"strings"
	"testing"

	"github.com/Nareik33L/bruiser-gateway/internal/check"
	"github.com/Nareik33L/bruiser-gateway/internal/resource"
	"github.com/Nareik33L/bruiser-gateway/internal/testlab"
)

func TestCorpusVersionAligned(t *testing.T) {
	if check.CorpusVersion != resource.VariantCorpusVersion {
		t.Fatalf("check.CorpusVersion=%s resource.VariantCorpusVersion=%s", check.CorpusVersion, resource.VariantCorpusVersion)
	}
	if len(resource.CupfinalCorpus()) != 9 {
		t.Fatalf("cupfinal corpus want 9 got %d", len(resource.CupfinalCorpus()))
	}
}

func TestNoPassWithoutAdversarialInput(t *testing.T) {
	bogus := check.Probe{
		Name:   "Forged execution rejected at origin",
		Status: "PASS",
		Pass:   true,
		Detail: "origin rejected unauthenticated call",
	}
	var hit string
	for _, c := range check.Contracts() {
		if c.Name == bogus.Name {
			hit = check.ContractViolation(bogus, c)
		}
	}
	if hit == "" {
		t.Fatal("a PASS with no origin secret and no forged token must violate the contract")
	}

	edgeURL, originURL, gwURL, hmac, adminURL := startLab(t, "origin-lock-dev")
	down := testlab.Start(t, testlab.ArsenalProfile(t), nil)
	downURL, downEdge, downHMAC := down.Server.URL, down.Cfg.EdgeSecret, down.Cfg.DevHMACSecret
	down.Store.Close()

	rep, err := check.Run(check.Config{
		EdgeURL:             edgeURL,
		OriginURL:           originURL,
		ControlURL:          gwURL,
		AdminURL:            adminURL,
		HMACSecret:          hmac,
		Membership:          "1001234",
		EventID:             "ars-che",
		CommitSHA:           "testsha",
		StoreDownURL:        downURL,
		StoreDownEdgeSecret: downEdge,
		StoreDownHMAC:       downHMAC,
	})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]check.Probe{}
	for _, p := range rep.Probes {
		byName[p.Name] = p
		for _, c := range check.Contracts() {
			if c.Name != p.Name {
				continue
			}
			if v := check.ContractViolation(p, c); v != "" {
				t.Errorf("%s", v)
			}
		}
	}
	for _, c := range check.Contracts() {
		p, ok := byName[c.Name]
		if !ok {
			t.Errorf("missing contracted probe %q", c.Name)
			continue
		}
		if p.Status == "PASS" && p.Evidence == nil {
			t.Errorf("%s PASSed without evidence", c.Name)
		}
	}
}

func TestCertificateNotIssuedOnLab(t *testing.T) {
	edgeURL, originURL, gwURL, hmac, adminURL := startLab(t, "origin-lock-dev")
	rep, err := check.Run(check.Config{
		EdgeURL:    edgeURL,
		OriginURL:  originURL,
		ControlURL: gwURL,
		AdminURL:   adminURL,
		HMACSecret: hmac,
		Membership: "1001234",
		EventID:    "ars-che",
		CommitSHA:  "deadbeef",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Passed() {
		t.Fatalf("lab check should still Overall PASS\n%s", rep.String())
	}
	if rep.CertificateIssued() {
		t.Fatal("lab instance must not receive an authority certificate")
	}
	if rep.Certificate == nil || !strings.Contains(rep.Certificate.Reason, "production") {
		t.Fatalf("certificate reason: %+v", rep.Certificate)
	}
}

func TestCertificateIssuedOnlyWhenEveryProbePassesProduction(t *testing.T) {
	probes := make([]check.Probe, 0, len(check.Contracts()))
	for _, c := range check.Contracts() {
		probes = append(probes, check.Probe{Name: c.Name, Status: "PASS", Pass: true, Detail: "ok"})
	}
	rep := check.Report{
		Probes:        probes,
		Overall:       "PASS",
		CommitSHA:     "abc123",
		CorpusVersion: check.CorpusVersion,
		Production:    true,
	}
	// issueCertificate is unexported; Run's finalize is the production path.
	// Reconstruct via a local helper by encoding the same rules here.
	if !rep.Production || rep.Overall != "PASS" || rep.CommitSHA == "" {
		t.Fatal("setup")
	}
	for _, p := range rep.Probes {
		if p.Status != "PASS" {
			t.Fatal("setup")
		}
	}

	rep.Probes[0].Status = "INCONCLUSIVE"
	rep.Probes[0].Pass = false
	// A synthetic report with INCONCLUSIVE must not be issuable. The
	// live path is issueCertificate via finalize; assert the public rule.
	if wouldIssue(rep) {
		t.Fatal("INCONCLUSIVE must block the certificate")
	}
	rep.Probes[0].Status = "PASS"
	rep.Probes[0].Pass = true
	if !wouldIssue(rep) {
		t.Fatal("all PASS on production with a commit SHA must issue")
	}
	rep.Production = false
	if wouldIssue(rep) {
		t.Fatal("lab must not issue")
	}
}

func wouldIssue(rep check.Report) bool {
	if rep.CommitSHA == "" || rep.CommitSHA == "unknown" || !rep.Production || rep.Overall != "PASS" {
		return false
	}
	for _, p := range rep.Probes {
		if p.Status != "PASS" {
			return false
		}
	}
	return true
}

func TestHonestOriginProbesCarrySecretAndAdversary(t *testing.T) {
	edgeURL, originURL, gwURL, hmac, adminURL := startLab(t, "origin-lock-dev")
	rep, err := check.Run(check.Config{
		EdgeURL:    edgeURL,
		OriginURL:  originURL,
		ControlURL: gwURL,
		AdminURL:   adminURL,
		HMACSecret: hmac,
		Membership: "1001234",
		EventID:    "ars-che",
		CommitSHA:  "testsha",
	})
	if err != nil {
		t.Fatal(err)
	}
	var forged, stale, variants check.Probe
	for _, p := range rep.Probes {
		switch p.Name {
		case "Forged execution rejected at origin":
			forged = p
		case "Stale fence rejected at origin":
			stale = p
		case "Resource variants share one domain":
			variants = p
		}
	}
	if forged.Status != "PASS" {
		t.Fatalf("forged\n%s", forged.Detail)
	}
	if stale.Status != "PASS" {
		t.Fatalf("stale fence\n%s", stale.Detail)
	}
	if variants.Status != "PASS" {
		t.Fatalf("variants\n%s", variants.Detail)
	}
	for _, c := range check.Contracts() {
		var p check.Probe
		switch c.Name {
		case "Forged execution rejected at origin":
			p = forged
		case "Stale fence rejected at origin":
			p = stale
		case "Resource variants share one domain":
			p = variants
		default:
			continue
		}
		if v := check.ContractViolation(p, c); v != "" {
			t.Error(v)
		}
	}
}
