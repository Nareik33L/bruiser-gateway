package publicapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/id"
	"github.com/Nareik33L/bruiser-gateway/internal/identity"
	"github.com/Nareik33L/bruiser-gateway/internal/lease"
	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
	"github.com/Nareik33L/bruiser-gateway/internal/policy"
	rescanon "github.com/Nareik33L/bruiser-gateway/internal/resource"
	pgstore "github.com/Nareik33L/bruiser-gateway/internal/store/postgres"
)

type authorizeReq struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	EventID string `json:"event_id"`
}

type admitResult struct {
	status  int
	allow   bool
	headers http.Header
	body    any
	exeID   string
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	if s.cfg.EdgeSecret == "" || !s.edgeAuthorized(r) {
		writeErr(w, http.StatusUnauthorized, "bad edge secret")
		return
	}
	if s.rates != nil && !s.rates.check(w, "authorize", clientIP(r), s.rates.authorize) {
		return
	}
	if s.rates != nil && !s.rates.check(w, "merchant", s.cfg.MerchantID, s.rates.merchant) {
		return
	}
	if !s.claimReplay(w, r, "authorize") {
		return
	}
	method := r.Header.Get("X-Original-Method")
	path := r.Header.Get("X-Original-URI")
	eventID := r.Header.Get("X-Bruiser-Event-Id")
	if method == "" || path == "" {
		var body authorizeReq
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body)
		if body.Method != "" {
			method = body.Method
		}
		if body.Path != "" {
			path = body.Path
		}
		if body.EventID != "" {
			eventID = body.EventID
		}
	}
	if u, err := url.Parse(path); err == nil && path != "" {
		path = u.Path
	}
	if method == "" {
		method = r.Method
	}
	res := s.admit(r.Context(), method, path, eventID, r, requestID(r))
	writeAdmit(w, res)
}

func writeAdmit(w http.ResponseWriter, res admitResult) {
	for k, vs := range res.headers {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	if res.body == nil {
		w.WriteHeader(res.status)
		return
	}
	writeJSON(w, res.status, res.body)
}

func (s *Server) admit(ctx context.Context, method, path, eventID string, r *http.Request, reqID string) admitResult {
	h := make(http.Header)
	route, params, ok := s.profile.MatchRoute(method, path)
	if !ok {
		if s.profile.UnmatchedAllow() {
			h.Set("X-Bruiser-Control", "none")
			return admitResult{status: http.StatusOK, allow: true, headers: h, body: map[string]string{"status": "ALLOW", "action": "unmatched"}}
		}
		return admitResult{status: http.StatusForbidden, body: map[string]string{"error": "unrouted", "path": path}}
	}
	if !route.Controlled() {
		h.Set("X-Bruiser-Control", "none")
		return admitResult{status: http.StatusOK, allow: true, headers: h, body: map[string]string{"status": "ALLOW", "action": route.Action}}
	}

	cust, err := identity.ExtractInput(ctx, s.identityInput(r, true))
	if err != nil {
		s.eaf.record(resourceOrPath(route, params, eventID), "", "unauthorized")
		return admitResult{status: http.StatusUnauthorized, body: map[string]string{"error": "missing merchant session"}}
	}

	resource := route.ResourceFor(params, eventID)
	if resource == "" {
		return admitResult{status: http.StatusBadRequest, body: map[string]string{"error": "could not derive resource"}}
	}
	if canon, err := s.canonicalizeResource(resource); err != nil {
		msg := "malformed resource"
		if err == rescanon.ErrUnknownResource {
			msg = "unknown resource"
		}
		return admitResult{status: http.StatusBadRequest, body: map[string]string{"error": msg}}
	} else if canon != "" {
		resource = canon
	}

	d := s.evaluate(s.cfg.MerchantID, cust.CustomerID, "agent", resource, route.Action, cust.Anchors)
	d, ctl := s.applyControls(d, route.Action)
	if d.ControlNone {
		h.Set("X-Bruiser-Control", "none")
		return admitResult{status: http.StatusOK, allow: true, headers: h, body: map[string]string{"status": "ALLOW", "action": route.Action, "rule_name": d.RuleName}}
	}
	if d.Denied {
		s.eaf.record(resource, cust.CustomerID, "denied")
		reason := d.Reason
		if reason == "" {
			reason = "denied"
		}
		s.recordDecision(ctx, false, cust.CustomerID, "", resource, route.Action, d.RuleName, "DENIED", reason, reqID, "")
		return admitResult{status: http.StatusForbidden, body: map[string]any{"error": "DENIED", "reason": reason, "rule_name": d.RuleName}}
	}

	ramp := s.rampInput(cust.CustomerID, resource, route.Action, d.RuleName, eventID, path, cust.Anchors)
	if !ctl.ShouldEnforce(ramp) {
		return s.admitDryRun(ctx, h, cust.CustomerID, cust.PrincipalID, resource, route.Action, d, reqID, ctl.EffectivePercent())
	}

	principalID := cust.PrincipalID
	if principalID == "" {
		principalID = id.New("p")
	}
	principalID = "unaware:" + principalID
	sessID := id.Session()
	exp := time.Now().UTC().Add(s.cfg.SessionTTL)
	if err := s.store.InsertSession(ctx, pgstore.SessionRow{
		ID:            sessID,
		MerchantID:    s.cfg.MerchantID,
		CustomerID:    cust.CustomerID,
		Anchors:       cust.Anchors,
		PrincipalType: "agent",
		PrincipalID:   principalID,
		ExpiresAt:     exp,
	}); err != nil {
		return s.admitStoreError(err)
	}

	rule := d.RuleName
	acq, err := s.leases.Acquire(ctx, lease.AcquireRequest{
		MerchantID:  s.cfg.MerchantID,
		DomainKey:   d.DomainKey,
		CustomerID:  cust.CustomerID,
		Principal:   lease.Principal{Type: "agent", ID: principalID},
		SessionID:   sessID,
		Resource:    resource,
		Action:      route.Action,
		RuleName:    rule,
		MaxActive:   d.MaxActive,
		TTL:         d.TTL,
		MaxLifetime: d.MaxLifetime,
		Precedence:  d.Precedence,
		MaxWaiters:  d.MaxWaiters,
		MaxOps:      d.MaxOps,
		RequestID:   reqID,
	})
	if err != nil {
		return s.admitStoreError(err)
	}
	acquireTotal.WithLabelValues("transparent_"+acq.Status, rule).Inc()
	s.recordAcquire(ctx, true, cust.CustomerID, principalID, resource, route.Action, rule, acq, reqID)
	h.Set("X-Bruiser-Enforced", "1")
	h.Set("X-Bruiser-Ramp", strconv.Itoa(ctl.EffectivePercent()))

	switch acq.Status {
	case lease.StatusGranted, lease.StatusAlreadyHeld:
		outcome := "allow"
		if acq.Status == lease.StatusAlreadyHeld {
			outcome = "resume"
		}
		s.eaf.record(resource, cust.CustomerID, outcome)
		tok, err := s.signer.SignExecution(*acq.Execution)
		if err != nil {
			return admitResult{status: http.StatusInternalServerError, body: map[string]string{"error": "sign"}}
		}
		h.Set("X-Bruiser-Execution", tok)
		h.Set("X-Bruiser-Fence", strconv.FormatInt(acq.Execution.Fence, 10))
		h.Set("X-Bruiser-Customer", cust.CustomerID)
		_ = s.store.EnsureBudget(ctx, s.cfg.MerchantID, acq.Execution.ID, d.MaxOps)
		if _, _, err := s.store.ConsumeOp(ctx, s.cfg.MerchantID, acq.Execution.ID); err != nil {
			return admitResult{status: http.StatusConflict, body: map[string]string{"status": "DENIED", "error": "BUDGET"}}
		}
		_ = s.store.WriteAudit(ctx, s.cfg.MerchantID, "AUTHORIZE", acq.Status, reqID, map[string]any{
			"customer_id":  cust.CustomerID,
			"execution_id": acq.Execution.ID,
			"resource":     resource,
			"action":       route.Action,
		})
		return admitResult{
			status:  http.StatusOK,
			allow:   true,
			headers: h,
			exeID:   acq.Execution.ID,
			body: map[string]any{
				"status":             "ALLOW",
				"execution_id":       acq.Execution.ID,
				"fence":              acq.Execution.Fence,
				"customer_id":        cust.CustomerID,
				"expires_at":         acq.Execution.ExpiresAt.UTC().Format(time.RFC3339Nano),
				"heartbeat_after_ms": s.cfg.HeartbeatInterval.Milliseconds(),
			},
		}
	case lease.StatusQueued:
		s.eaf.record(resource, cust.CustomerID, "queued")
		retry := time.Until(acq.Busy.ExpiresAt).Milliseconds()
		if retry < 0 {
			retry = 1000
		}
		h.Set("Retry-After", "2")
		return admitResult{
			status:  http.StatusAccepted,
			headers: h,
			body: map[string]any{
				"status":              "QUEUED",
				"execution_id":        acq.Queue.WaiterID,
				"position":            acq.Queue.Position,
				"active_execution_id": acq.Queue.ActiveExecutionID,
				"expires_at":          acq.Queue.ExpiresAt.UTC().Format(time.RFC3339Nano),
				"retry_after_ms":      retry,
			},
		}
	case lease.StatusBusy:
		s.eaf.record(resource, cust.CustomerID, "busy")
		retry := time.Until(acq.Busy.ExpiresAt).Milliseconds()
		if retry < 0 {
			retry = 1000
		}
		h.Set("Retry-After", "2")
		return admitResult{
			status:  http.StatusConflict,
			headers: h,
			body: map[string]any{
				"status":              "BUSY",
				"active_execution_id": acq.Busy.ActiveExecutionID,
				"holder":              map[string]string{"type": acq.Busy.Holder.Type, "id": acq.Busy.Holder.ID},
				"expires_at":          acq.Busy.ExpiresAt.UTC().Format(time.RFC3339Nano),
				"retry_after_ms":      retry,
			},
		}
	default:
		s.eaf.record(resource, cust.CustomerID, "denied")
		return admitResult{status: http.StatusForbidden, body: map[string]string{"error": acq.Reason}}
	}
}

func (s *Server) admitDryRun(ctx context.Context, h http.Header, customerID, principalID, resource, action string, d policy.Decision, reqID string, percent int) admitResult {
	if principalID == "" {
		principalID = id.New("p")
	}
	principalID = "unaware:" + principalID
	ev, err := s.store.ShadowDecide(ctx, s.cfg.MerchantID, d.DomainKey, customerID, principalID, resource, action, d.RuleName, d.MaxActive, d.MaxWaiters, d.TTL, reqID)
	would, reason := "WOULD_UNKNOWN", "store_unavailable"
	if err == nil {
		would, reason = "WOULD_"+ev.Would, ev.Reason
	}
	s.eaf.record(resource, customerID, "would_"+strings.ToLower(strings.TrimPrefix(would, "WOULD_")))
	if h == nil {
		h = make(http.Header)
	}
	h.Set("X-Bruiser-Control", "observe")
	h.Set("X-Bruiser-Dry-Run", would)
	h.Set("X-Bruiser-Dry-Run-Reason", reason)
	h.Set("X-Bruiser-Enforced", "0")
	h.Set("X-Bruiser-Ramp", strconv.Itoa(percent))
	return admitResult{
		status:  http.StatusOK,
		allow:   true,
		headers: h,
		body: map[string]any{
			"status":          "ALLOW",
			"would":           would,
			"reason":          reason,
			"action":          action,
			"customer_id":     customerID,
			"rule_name":       d.RuleName,
			"enforced":        false,
			"enforce_percent": percent,
		},
	}
}

func (s *Server) admitStoreError(err error) admitResult {
	if s.failOpen() {
		h := make(http.Header)
		h.Set("X-Bruiser-Control", "fail-open")
		return admitResult{status: http.StatusOK, allow: true, headers: h, body: map[string]any{"status": "ALLOW", "control": "fail-open"}}
	}
	switch {
	case err == nil:
		return admitResult{status: http.StatusInternalServerError, body: map[string]string{"error": "internal error"}}
	default:
		// Mirror storeError without a ResponseWriter.
		buf := &statusRecorder{code: http.StatusInternalServerError, body: &bytes.Buffer{}}
		s.storeError(buf, err)
		var payload any
		_ = json.Unmarshal(buf.body.Bytes(), &payload)
		h := make(http.Header)
		if ra := buf.Header().Get("Retry-After"); ra != "" {
			h.Set("Retry-After", ra)
		}
		if payload == nil {
			payload = map[string]string{"error": "internal error"}
		}
		return admitResult{status: buf.code, headers: h, body: payload}
	}
}

type statusRecorder struct {
	code  int
	hdr   http.Header
	body  *bytes.Buffer
	wrote bool
}

func (s *statusRecorder) Header() http.Header {
	if s.hdr == nil {
		s.hdr = make(http.Header)
	}
	return s.hdr
}
func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wrote {
		s.WriteHeader(http.StatusOK)
	}
	return s.body.Write(b)
}
func (s *statusRecorder) WriteHeader(status int) {
	if s.wrote {
		return
	}
	s.wrote = true
	s.code = status
}

func cookieValue(r *http.Request, name string) string {
	if r == nil || name == "" {
		return ""
	}
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

func headerValue(r *http.Request, name string) string {
	if r == nil || name == "" {
		return ""
	}
	return r.Header.Get(name)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func resourceOrPath(route merchant.Route, params map[string]string, eventID string) string {
	if res := route.ResourceFor(params, eventID); res != "" {
		return res
	}
	return route.Match.Path
}

func (s *Server) stampOrigin(h http.Header) {
	if s.cfg.OriginSecret != "" && h != nil {
		h.Set("X-Bruiser-Origin-Secret", s.cfg.OriginSecret)
	}
}

func eventIDFromBody(body []byte) string {
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return ""
	}
	if v, ok := m["event_id"].(string); ok {
		return v
	}
	return ""
}
