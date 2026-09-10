package publicapi

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/Nareik33L/bruiser-gateway/internal/lease"
	pgstore "github.com/Nareik33L/bruiser-gateway/internal/store/postgres"
)

// ReasonAdminReset is the end_reason recorded when an operator clears executions.
const ReasonAdminReset = "ADMIN_RESET"

// operatorAuth gates the operator group behind a shared secret that is distinct
// from the edge secret. The group is not mounted when the secret is unset.
func (s *Server) operatorAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-Bruiser-Operator-Secret")
		if got == "" {
			got = bearer(r)
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.OperatorSecret)) != 1 {
			writeErr(w, http.StatusUnauthorized, "bad operator secret")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) mountOperator(r chi.Router) {
	r.Route("/operator", func(r chi.Router) {
		r.Use(s.operatorAuth)
		r.Get("/stats", s.operatorStats)
		r.Get("/executions", s.operatorExecutions)
		r.Get("/audit", s.operatorAudit)
		r.Post("/reset-executions", s.operatorResetExecutions)
		r.Post("/eaf/reset", s.operatorResetEAF)
	})
}

func (a *eafAcc) reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for res := range a.attempts {
		observedEAF.DeleteLabelValues(res)
		downstreamEAF.DeleteLabelValues(res)
	}
	a.attempts = map[string]float64{}
	a.forwarded = map[string]float64{}
	a.customers = map[string]map[string]struct{}{}
	a.outcomes = map[string]map[string]float64{}
}

func (s *Server) operatorStats(w http.ResponseWriter, r *http.Request) {
	active, err := s.store.CountActive(r.Context(), s.cfg.MerchantID)
	if err != nil {
		s.storeError(w, err)
		return
	}
	body := map[string]any{
		"merchant_id":       s.cfg.MerchantID,
		"active_executions": active,
		"eaf":               s.eaf.snapshotAll(),
	}
	if cache, ok := s.leases.(*lease.BusyCache); ok && cache != nil {
		body["busy_cache"] = map[string]uint64{
			"hits":   cache.Hits(),
			"misses": cache.Misses(),
		}
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) operatorExecutions(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := s.store.ListActive(r.Context(), s.cfg.MerchantID, "", limit)
	if err != nil {
		s.storeError(w, err)
		return
	}
	out := make([]map[string]any, 0, len(list))
	for i := range list {
		e := list[i]
		out = append(out, map[string]any{
			"execution_id": e.ID,
			"customer_id":  e.CustomerID,
			"principal":    map[string]string{"type": e.Principal.Type, "id": e.Principal.ID},
			"resource":     e.Resource,
			"action":       e.Action,
			"fence":        e.Fence,
			"granted_at":   e.GrantedAt.UTC(),
			"expires_at":   e.ExpiresAt.UTC(),
			"rule_name":    e.RuleName,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"executions": out, "count": len(out)})
}

func (s *Server) operatorAudit(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, err := s.store.ListAudit(r.Context(), s.cfg.MerchantID, limit)
	if err != nil {
		s.storeError(w, err)
		return
	}
	if events == nil {
		events = []pgstore.AuditEvent{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events, "count": len(events)})
}

// operatorResetExecutions revokes every live execution for the merchant through
// the lease store so the busy cache forgets each domain. Audit rows are written
// by the store; nothing is deleted.
func (s *Server) operatorResetExecutions(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListActive(r.Context(), s.cfg.MerchantID, "", 10000)
	if err != nil {
		s.storeError(w, err)
		return
	}
	revoked := 0
	for i := range list {
		if _, err := s.leases.Revoke(r.Context(), s.cfg.MerchantID, list[i].ID, list[i].SessionID, requestID(r), ReasonAdminReset); err != nil {
			var gone *lease.GoneError
			if errors.As(err, &gone) || errors.Is(err, lease.ErrNotFound) {
				continue
			}
			s.storeError(w, err)
			return
		}
		revoked++
	}
	if r.URL.Query().Get("keep_eaf") == "" {
		s.eaf.reset()
	}
	s.log.Info("operator reset executions", "merchant", s.cfg.MerchantID, "revoked", revoked)
	writeJSON(w, http.StatusOK, map[string]any{"status": "reset", "revoked": revoked})
}

func (s *Server) operatorResetEAF(w http.ResponseWriter, _ *http.Request) {
	s.eaf.reset()
	writeJSON(w, http.StatusOK, map[string]string{"status": "reset"})
}
