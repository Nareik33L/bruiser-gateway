package publicapi

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/lease"
	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
	"github.com/Nareik33L/bruiser-gateway/internal/policy"
	pgstore "github.com/Nareik33L/bruiser-gateway/internal/store/postgres"
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
				s.policyVer.Store(int64(row.Version))
			}
		}
	}
	s.compiled.Store(&c)
}

func (s *Server) CheckPolicySafety() error {
	if s == nil {
		return nil
	}
	if err := merchant.ValidateProductionPolicy(s.profile, s.cfg.AllowUnsafeModes); err != nil && s.cfg.Production() {
		return err
	}
	if s.cfg.Production() {
		return policy.CheckProductionSafety(s.getCompiled().Doc, s.cfg.AllowUnsafeModes)
	}
	return nil
}

func (s *Server) applyStorePolicy() {
	if s.store == nil {
		return
	}
	row, err := s.store.ActivePolicy(context.Background(), s.cfg.MerchantID)
	if err != nil {
		return
	}
	if row.Version == int(s.policyVer.Load()) {
		return
	}
	loaded, err := policy.CompileYAML([]byte(row.YAML))
	if err != nil {
		return
	}
	s.compiled.Store(&loaded)
	s.policyVer.Store(int64(row.Version))
	if cache, ok := s.leases.(*lease.BusyCache); ok {
		cache.Reset()
	}
}

func (s *Server) Start(ctx context.Context) {
	if s == nil || s.store == nil {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	s.stopWatch = cancel
	go s.store.ListenPolicyLoop(ctx, func(merchantID string) {
		if merchantID != "" && merchantID != s.cfg.MerchantID {
			return
		}
		s.applyStorePolicy()
	})
	go s.pollPolicy(ctx)
}

func (s *Server) Close() {
	if s != nil && s.stopWatch != nil {
		s.stopWatch()
	}
}

func (s *Server) pollPolicy(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.applyStorePolicy()
		}
	}
}

func (s *Server) PolicyVersion() int {
	return int(s.policyVer.Load())
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

func (s *Server) getPolicy(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, roleOperator); !ok {
		return
	}
	c := s.getCompiled()
	rules := make([]string, 0, len(c.Doc.Domains))
	for _, d := range c.Doc.Domains {
		rules = append(rules, d.Name)
	}
	body := map[string]any{
		"version":    s.PolicyVersion(),
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
	actor, ok := s.requireRole(w, r, roleAdmin)
	if !ok {
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
	if s.cfg.Production() {
		if err := policy.CheckProductionSafety(c.Doc, s.cfg.AllowUnsafeModes); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "fail-open policy", "detail": err.Error()})
			return
		}
	}
	beforeYAML := ""
	if prev, err := s.store.ActivePolicy(r.Context(), s.cfg.MerchantID); err == nil {
		beforeYAML = prev.YAML
	}
	beforeHash := policy.HashYAML([]byte(beforeYAML))
	afterHash := policy.HashYAML(raw)
	row, err := s.store.PutPolicy(r.Context(), s.cfg.MerchantID, string(raw), requestID(r), pgstore.AdminAudit{
		Actor:     actor.Actor,
		Role:      actor.Role,
		IP:        clientIP(r),
		Auth:      actor.Source,
		Before:    beforeHash,
		After:     afterHash,
		RequestID: requestID(r),
	})
	if err != nil {
		s.storeError(w, err)
		return
	}
	s.compiled.Store(&c)
	s.policyVer.Store(int64(row.Version))
	if cache, ok := s.leases.(*lease.BusyCache); ok {
		cache.Reset()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":     row.Version,
		"rule_names":  ruleNames(c),
		"status":      "active",
		"before_hash": beforeHash,
		"after_hash":  afterHash,
	})
}

func (s *Server) policyHistory(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireRole(w, r, roleOperator); !ok {
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
