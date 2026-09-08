package publicapi

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/lease"
	"github.com/Nareik33L/bruiser-gateway/internal/resource"
	pgstore "github.com/Nareik33L/bruiser-gateway/internal/store/postgres"
)

func (s *Server) introspect(w http.ResponseWriter, r *http.Request) {
	raw := bearer(r)
	var body struct {
		Token    string `json:"token"`
		Resource string `json:"resource"`
		Consume  bool   `json:"consume"`
	}
	if raw == "" {
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body)
		raw = body.Token
	}
	if raw == "" {
		writeErr(w, http.StatusUnauthorized, "missing execution token")
		return
	}
	claims, err := auth.ParseExecution(raw, s.signer.Public)
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid execution token")
		return
	}
	if claims.MerchantID != s.cfg.MerchantID {
		writeErr(w, http.StatusUnauthorized, "wrong merchant")
		return
	}
	if body.Resource != "" {
		want, err := resource.Canonical(body.Resource)
		got, gerr := resource.Canonical(claims.Resource)
		if err != nil || gerr != nil || want != got {
			writeErr(w, http.StatusForbidden, "resource mismatch")
			return
		}
	}
	e, err := s.leases.Get(r.Context(), s.cfg.MerchantID, claims.ExecutionID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"active": false, "status": "DENIED", "reason": "not_found"})
		return
	}
	if e.Fence > claims.Fence {
		writeJSON(w, http.StatusOK, map[string]any{"active": false, "status": "DENIED", "reason": "stale_fence", "fence": e.Fence})
		return
	}
	if e.State == lease.StateRevoked || e.State == lease.StateHandedOff {
		writeJSON(w, http.StatusOK, map[string]any{"active": false, "status": "DENIED", "reason": e.State, "execution_id": e.ID})
		return
	}
	active := e.IsActive(time.Now().UTC())
	if active && body.Consume {
		if _, _, err := s.store.ConsumeOp(r.Context(), s.cfg.MerchantID, e.ID); err == pgstore.ErrBudget {
			writeJSON(w, http.StatusOK, map[string]any{"active": false, "status": "DENIED", "reason": "BUDGET"})
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"active":       active,
		"status":       map[bool]string{true: "ALLOW", false: "DENIED"}[active],
		"execution_id": claims.ExecutionID,
		"customer_id":  claims.CustomerID,
		"merchant_id":  claims.MerchantID,
		"resource":     claims.Resource,
		"action":       claims.Action,
		"fence":        claims.Fence,
		"domain":       claims.Domain,
		"state":        e.State,
	})
}
