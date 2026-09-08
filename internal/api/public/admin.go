package publicapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Nareik33L/bruiser-gateway/internal/check"
	"github.com/Nareik33L/bruiser-gateway/internal/id"
	"github.com/Nareik33L/bruiser-gateway/internal/lease"
)

func (s *Server) adminPage(w http.ResponseWriter, r *http.Request) {
	if !s.adminOK(r) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, adminLoginHTML)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(adminHTML))
}

func (s *Server) adminStatus(w http.ResponseWriter, r *http.Request) {
	if !s.adminOK(r) {
		writeErr(w, http.StatusUnauthorized, "bad admin secret")
		return
	}
	writeJSON(w, http.StatusOK, s.statusPayload(r.Context()))
}

func (s *Server) statusPayload(ctx context.Context) map[string]any {
	obs, down, att, fwd := s.eaf.headline()
	absorbed := att - fwd
	if absorbed < 0 {
		absorbed = 0
	}
	usage, _ := s.store.UsageFigures(ctx, s.cfg.MerchantID)
	active, _ := s.store.ListActive(ctx, s.cfg.MerchantID, "", 50)
	execs := make([]map[string]any, 0, len(active))
	for _, e := range active {
		execs = append(execs, map[string]any{
			"execution_id": e.ID,
			"customer_id":  e.CustomerID,
			"principal":    map[string]string{"type": e.Principal.Type, "id": e.Principal.ID},
			"resource":     e.Resource,
			"action":       e.Action,
			"fence":        e.Fence,
			"rule_name":    e.RuleName,
			"expires_at":   e.ExpiresAt.UTC().Format(time.RFC3339Nano),
		})
	}
	waiters, _ := s.store.ListWaiters(ctx, s.cfg.MerchantID, "", 50)
	queued := make([]map[string]any, 0, len(waiters))
	for _, e := range waiters {
		queued = append(queued, map[string]any{
			"execution_id": e.ID,
			"customer_id":  e.CustomerID,
			"principal":    map[string]string{"type": e.Principal.Type, "id": e.Principal.ID},
			"resource":     e.Resource,
			"action":       e.Action,
			"rule_name":    e.RuleName,
			"expires_at":   e.ExpiresAt.UTC().Format(time.RFC3339Nano),
		})
	}
	queueDepth.WithLabelValues(s.cfg.MerchantID).Set(float64(usage.QueueDepth))
	body := map[string]any{
		"merchant":       s.cfg.MerchantID,
		"version":        "0.1.0-dev",
		"telemetry":      s.cfg.Telemetry,
		"policy_version": s.PolicyVersion(),
		"eaf": map[string]any{
			"observed":   obs,
			"downstream": down,
			"attempts":   att,
			"forwarded":  fwd,
			"absorbed":   absorbed,
			"resources":  s.eaf.snapshotAll(),
		},
		"usage":      usage,
		"executions": execs,
		"waiters":    queued,
	}
	if ev, err := s.store.LastAudit(ctx, s.cfg.MerchantID, "AUTHORITY_CHECK"); err == nil {
		body["last_authority_check"] = map[string]any{
			"at":     ev.At.UTC().Format(time.RFC3339Nano),
			"reason": ev.Reason,
			"report": ev.Attrs,
		}
	}
	return body
}

func (s *Server) adminStream(w http.ResponseWriter, r *http.Request) {
	if !s.adminOK(r) {
		writeErr(w, http.StatusUnauthorized, "bad admin secret")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "stream unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	enc := json.NewEncoder(w)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			_, _ = io.WriteString(w, "event: status\ndata: ")
			_ = enc.Encode(s.statusPayload(r.Context()))
			_, _ = io.WriteString(w, "\n")
			flusher.Flush()
		}
	}
}

func (s *Server) adminAudit(w http.ResponseWriter, r *http.Request) {
	if !s.adminOK(r) {
		writeErr(w, http.StatusUnauthorized, "bad admin secret")
		return
	}
	customer := r.URL.Query().Get("customer")
	typ := r.URL.Query().Get("type")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rows, err := s.store.SearchAudit(r.Context(), s.cfg.MerchantID, customer, typ, limit)
	if err != nil {
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": rows})
}

func (s *Server) adminExport(w http.ResponseWriter, r *http.Request) {
	if !s.adminOK(r) {
		writeErr(w, http.StatusUnauthorized, "bad admin secret")
		return
	}
	since := time.Now().UTC().Add(-24 * time.Hour)
	if v := r.URL.Query().Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			since = t
		}
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 {
		limit = 5000
	}
	rows, err := s.store.ExportAudit(r.Context(), s.cfg.MerchantID, since, limit)
	if err != nil {
		s.storeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", "attachment; filename=bruiser-audit.jsonl")
	enc := json.NewEncoder(w)
	for _, ev := range rows {
		_ = enc.Encode(ev)
	}
}

func (s *Server) adminCustomer(w http.ResponseWriter, r *http.Request) {
	if !s.adminOK(r) {
		writeErr(w, http.StatusUnauthorized, "bad admin secret")
		return
	}
	cid := chi.URLParam(r, "id")
	execs, err := s.store.ListActive(r.Context(), s.cfg.MerchantID, cid, 50)
	if err != nil {
		s.storeError(w, err)
		return
	}
	audit, err := s.store.SearchAudit(r.Context(), s.cfg.MerchantID, cid, "", 40)
	if err != nil {
		s.storeError(w, err)
		return
	}
	queued, _ := s.store.ListWaiters(r.Context(), s.cfg.MerchantID, cid, 50)
	writeJSON(w, http.StatusOK, map[string]any{
		"customer_id": cid,
		"executions":  execs,
		"waiters":     queued,
		"audit":       audit,
	})
}

func (s *Server) adminRevoke(w http.ResponseWriter, r *http.Request) {
	if !s.adminOK(r) {
		writeErr(w, http.StatusUnauthorized, "bad admin secret")
		return
	}
	var body revokeReq
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body)
	if body.Reason == "" {
		body.Reason = "admin"
	}
	e, err := s.leases.Revoke(r.Context(), s.cfg.MerchantID, chi.URLParam(r, "id"), "", requestID(r), body.Reason)
	if err != nil {
		s.mutationError(w, err)
		return
	}
	revokeTotal.Inc()
	writeJSON(w, http.StatusOK, map[string]any{
		"execution_id": e.ID,
		"state":        e.State,
		"end_reason":   e.EndReason,
	})
}

func (s *Server) adminLastCheck(w http.ResponseWriter, r *http.Request) {
	if !s.adminOK(r) {
		writeErr(w, http.StatusUnauthorized, "bad admin secret")
		return
	}
	ev, err := s.store.LastAudit(r.Context(), s.cfg.MerchantID, "AUTHORITY_CHECK")
	if err != nil {
		if err == lease.ErrNotFound {
			writeJSON(w, http.StatusOK, map[string]any{"last": nil})
			return
		}
		s.storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"at":     ev.At.UTC().Format(time.RFC3339Nano),
		"reason": ev.Reason,
		"report": ev.Attrs,
	})
}

func (s *Server) adminRunCheck(w http.ResponseWriter, r *http.Request) {
	if !s.adminOK(r) {
		writeErr(w, http.StatusUnauthorized, "bad admin secret")
		return
	}
	var body struct {
		EdgeURL   string `json:"edge_url"`
		OriginURL string `json:"origin_url"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body)
	if body.EdgeURL == "" {
		body.EdgeURL = s.cfg.CheckEdgeURL
	}
	if body.OriginURL == "" {
		body.OriginURL = s.cfg.CheckOriginURL
	}
	rep, err := check.Run(check.Config{
		EdgeURL:    body.EdgeURL,
		OriginURL:  body.OriginURL,
		HMACSecret: s.cfg.DevHMACSecret,
	})
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error()})
		return
	}
	_ = s.PersistAuthorityCheck(r.Context(), rep, requestID(r))
	status := http.StatusOK
	if !rep.Passed() {
		status = http.StatusConflict
	}
	writeJSON(w, status, rep)
}

func (s *Server) PersistAuthorityCheck(ctx context.Context, rep check.Report, requestID string) error {
	if requestID == "" {
		requestID = id.Request()
	}
	reason := rep.Overall
	attrs := map[string]any{
		"overall": rep.Overall,
		"probes":  rep.Probes,
		"covered": rep.Covered,
	}
	return s.store.WriteAudit(ctx, s.cfg.MerchantID, "AUTHORITY_CHECK", reason, requestID, attrs)
}
