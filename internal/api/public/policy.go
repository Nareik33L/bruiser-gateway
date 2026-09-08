package publicapi

import (
	"context"
	"crypto/subtle"
	"io"
	"net/http"

	"github.com/Nareik33L/bruiser-gateway/internal/lease"
	"github.com/Nareik33L/bruiser-gateway/internal/policy"
)

func (s *Server) loadPolicy() {
	doc := s.profile.PolicyDocument()
	c, err := policy.Compile(doc)
	if err != nil {
		c, _ = policy.Compile(policy.DefaultDocument(s.cfg.MerchantID))
	}
	if s.store != nil {
		if row, err := s.store.ActivePolicy(context.Background(), s.cfg.MerchantID); err == nil {
			if loaded, err := policy.CompileYAML([]byte(row.YAML)); err == nil {
				c = loaded
				s.policyVer = row.Version
			}
		}
	}
	s.compiled.Store(&c)
}

func (s *Server) getCompiled() policy.Compiled {
	p := s.compiled.Load()
	if p == nil {
		c, _ := policy.Compile(policy.DefaultDocument(s.cfg.MerchantID))
		return c
	}
	return *p
}

func (s *Server) evaluate(merchantID, customerID, ptype, resource, action string, anchors map[string]string) policy.Decision {
	return s.getCompiled().Evaluate(policy.Request{
		MerchantID:    merchantID,
		CustomerID:    customerID,
		PrincipalType: ptype,
		Resource:      resource,
		Action:        action,
		Anchors:       anchors,
	})
}

func (s *Server) adminOK(r *http.Request) bool {
	secret := s.cfg.EdgeSecret
	if secret == "" {
		return true
	}
	got := r.Header.Get("X-Bruiser-Admin-Secret")
	if got == "" {
		got = r.Header.Get("X-Bruiser-Edge-Secret")
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(secret)) == 1
}

func (s *Server) getPolicy(w http.ResponseWriter, r *http.Request) {
	if !s.adminOK(r) {
		writeErr(w, http.StatusUnauthorized, "bad admin secret")
		return
	}
	c := s.getCompiled()
	rules := make([]string, 0, len(c.Doc.Domains))
	for _, d := range c.Doc.Domains {
		rules = append(rules, d.Name)
	}
	body := map[string]any{
		"version":    s.policyVer,
		"merchant":   s.cfg.MerchantID,
		"rule_names": rules,
		"fallback":   c.Doc.Fallback,
	}
	if s.store != nil {
		if row, err := s.store.ActivePolicy(r.Context(), s.cfg.MerchantID); err == nil {
			body["version"] = row.Version
			body["yaml"] = row.YAML
		}
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) putPolicy(w http.ResponseWriter, r *http.Request) {
	if !s.adminOK(r) {
		writeErr(w, http.StatusUnauthorized, "bad admin secret")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil || len(raw) == 0 {
		writeErr(w, http.StatusBadRequest, "empty policy")
		return
	}
	c, err := policy.CompileYAML(raw)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid policy", "detail": err.Error()})
		return
	}
	row, err := s.store.PutPolicy(r.Context(), s.cfg.MerchantID, string(raw), requestID(r))
	if err != nil {
		s.storeError(w, err)
		return
	}
	s.compiled.Store(&c)
	s.policyVer = row.Version
	if cache, ok := s.leases.(*lease.BusyCache); ok {
		cache.Reset()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":    row.Version,
		"rule_names": ruleNames(c),
		"status":     "active",
	})
}

func (s *Server) policyHistory(w http.ResponseWriter, r *http.Request) {
	if !s.adminOK(r) {
		writeErr(w, http.StatusUnauthorized, "bad admin secret")
		return
	}
	rows, err := s.store.PolicyHistory(r.Context(), s.cfg.MerchantID, 20)
	if err != nil {
		s.storeError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{"version": row.Version, "active": row.Active})
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": out})
}

func ruleNames(c policy.Compiled) []string {
	names := make([]string, 0, len(c.Doc.Domains))
	for _, d := range c.Doc.Domains {
		names = append(names, d.Name)
	}
	return names
}
