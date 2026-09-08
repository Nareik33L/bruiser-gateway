package publicapi

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
)

func (s *Server) introspect(w http.ResponseWriter, r *http.Request) {
	raw := bearer(r)
	if raw == "" {
		var body struct {
			Token string `json:"token"`
		}
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
	active := false
	if e, err := s.leases.Get(r.Context(), s.cfg.MerchantID, claims.ExecutionID); err == nil {
		active = e.IsActive(time.Now().UTC())
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"active":       active,
		"execution_id": claims.ExecutionID,
		"customer_id":  claims.CustomerID,
		"resource":     claims.Resource,
		"action":       claims.Action,
		"fence":        claims.Fence,
		"domain":       claims.Domain,
	})
}
