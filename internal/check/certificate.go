package check

import (
	"os"
	"runtime/debug"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/resource"
)

// CorpusVersion is the Authority Check probe + variant corpus identifier.
// Bump when a probe is added/rewritten or the variant list changes.
const CorpusVersion = "1"

var _ = resource.VariantCorpusVersion

// Certificate is issued only when every probe PASSed on a
// production-configured instance. INCONCLUSIVE is never a PASS.
type Certificate struct {
	Issued        bool    `json:"issued"`
	Title         string  `json:"title,omitempty"`
	CommitSHA     string  `json:"commit_sha"`
	CorpusVersion string  `json:"corpus_version"`
	IssuedAt      string  `json:"issued_at,omitempty"`
	Production    bool    `json:"production"`
	Reason        string  `json:"reason,omitempty"`
	Probes        []Probe `json:"probes"`
}

func (r Report) CertificateIssued() bool {
	return r.Certificate != nil && r.Certificate.Issued
}

func issueCertificate(rep *Report) *Certificate {
	c := &Certificate{
		Title:         "Bruiser Authority Certificate",
		CommitSHA:     rep.CommitSHA,
		CorpusVersion: rep.CorpusVersion,
		Production:    rep.Production,
		Probes:        append([]Probe(nil), rep.Probes...),
	}
	if rep.CommitSHA == "" || rep.CommitSHA == "unknown" {
		c.Reason = "commit SHA is unknown; certificate requires a VCS-stamped binary"
		return c
	}
	if !rep.Production {
		c.Reason = "instance is not production-configured; lab/dev checks cannot issue a certificate"
		return c
	}
	if rep.Overall != "PASS" {
		c.Reason = "overall result is " + rep.Overall
		return c
	}
	for _, p := range rep.Probes {
		if p.Status != "PASS" {
			c.Reason = "probe " + p.Name + " is " + p.Status + " (certificate requires every probe to PASS)"
			return c
		}
	}
	c.Issued = true
	c.IssuedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return c
}

func resolveCommitSHA(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if v := os.Getenv("BRUISER_COMMIT_SHA"); v != "" {
		return v
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				return s.Value
			}
		}
	}
	return "unknown"
}
