package publicapi

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/id"
	"github.com/Nareik33L/bruiser-gateway/internal/lease"
	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
	pgstore "github.com/Nareik33L/bruiser-gateway/internal/store/postgres"
)

type authorizeReq struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	EventID string `json:"event_id"`
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request) {
	if s.cfg.EdgeSecret != "" {
		got := r.Header.Get("X-Bruiser-Edge-Secret")
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.EdgeSecret)) != 1 {
			writeErr(w, http.StatusUnauthorized, "bad edge secret")
			return
		}
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
	if u, err := url.Parse(path); err == nil {
		path = u.Path
	}
	if method == "" {
		method = r.Method
	}

	route, params, ok := s.profile.MatchRoute(method, path)
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "unrouted", "path": path})
		return
	}
	if !route.Controlled() {
		w.Header().Set("X-Bruiser-Control", "none")
		writeJSON(w, http.StatusOK, map[string]string{"status": "ALLOW", "action": route.Action})
		return
	}

	raw := cookieValue(r, s.profile.Identity.Cookie)
	if raw == "" {
		raw = bearer(r)
	}
	if raw == "" {
		s.eaf.record(resourceOrPath(route, params, eventID), "", "unauthorized")
		writeErr(w, http.StatusUnauthorized, "missing box office session")
		return
	}
	assertion, err := auth.ParseAssertionHS256(raw, s.cfg.DevHMACSecret, "bruiser")
	if err != nil {
		s.eaf.record(resourceOrPath(route, params, eventID), "", "unauthorized")
		writeErr(w, http.StatusUnauthorized, "invalid box office session")
		return
	}

	resource := route.ResourceFor(params, eventID)
	if resource == "" {
		writeErr(w, http.StatusBadRequest, "could not derive resource")
		return
	}

	principalID := assertion.JTI
	if principalID == "" {
		principalID = assertion.CustomerID
	}
	principalID = "unaware:" + principalID
	sessID := id.Session()
	exp := time.Now().UTC().Add(s.cfg.SessionTTL)
	if err := s.store.InsertSession(r.Context(), pgstore.SessionRow{
		ID:            sessID,
		MerchantID:    s.cfg.MerchantID,
		CustomerID:    assertion.CustomerID,
		Anchors:       assertion.Anchors,
		PrincipalType: "agent",
		PrincipalID:   principalID,
		ExpiresAt:     exp,
	}); err != nil {
		s.storeError(w, err)
		return
	}

	rule := s.profile.Policy.RuleName
	if rule == "" {
		rule = lease.DefaultRuleName()
	}
	maxActive := s.cfg.MaxActive
	if s.profile.Policy.MaxActive > 0 {
		maxActive = s.profile.Policy.MaxActive
	}
	domain := lease.DomainKey(s.cfg.MerchantID, rule, assertion.CustomerID, resource)
	res, err := s.leases.Acquire(r.Context(), lease.AcquireRequest{
		MerchantID:  s.cfg.MerchantID,
		DomainKey:   domain,
		CustomerID:  assertion.CustomerID,
		Principal:   lease.Principal{Type: "agent", ID: principalID},
		SessionID:   sessID,
		Resource:    resource,
		Action:      route.Action,
		RuleName:    rule,
		MaxActive:   maxActive,
		TTL:         s.cfg.LeaseTTL,
		MaxLifetime: s.cfg.MaxLifetime,
		RequestID:   requestID(r),
	})
	if err != nil {
		s.storeError(w, err)
		return
	}
	acquireTotal.WithLabelValues("transparent_"+res.Status, rule).Inc()

	switch res.Status {
	case lease.StatusGranted, lease.StatusAlreadyHeld:
		s.eaf.record(resource, assertion.CustomerID, "allow")
		tok, err := s.signer.SignExecution(*res.Execution)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "sign")
			return
		}
		w.Header().Set("X-Bruiser-Execution", tok)
		w.Header().Set("X-Bruiser-Fence", strconv.FormatInt(res.Execution.Fence, 10))
		w.Header().Set("X-Bruiser-Customer", assertion.CustomerID)
		if s.cfg.OriginSecret != "" {
			w.Header().Set("X-Bruiser-Origin-Secret", s.cfg.OriginSecret)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":       "ALLOW",
			"execution_id": res.Execution.ID,
			"fence":        res.Execution.Fence,
			"customer_id":  assertion.CustomerID,
		})
	case lease.StatusBusy:
		s.eaf.record(resource, assertion.CustomerID, "busy")
		retry := time.Until(res.Busy.ExpiresAt).Milliseconds()
		if retry < 0 {
			retry = 1000
		}
		w.Header().Set("Retry-After", "2")
		writeJSON(w, http.StatusConflict, map[string]any{
			"status":              "BUSY",
			"active_execution_id": res.Busy.ActiveExecutionID,
			"holder":              map[string]string{"type": res.Busy.Holder.Type, "id": res.Busy.Holder.ID},
			"expires_at":          res.Busy.ExpiresAt.UTC().Format(time.RFC3339Nano),
			"retry_after_ms":      retry,
		})
	default:
		s.eaf.record(resource, assertion.CustomerID, "denied")
		writeErr(w, http.StatusForbidden, res.Reason)
	}
}

func cookieValue(r *http.Request, name string) string {
	if name == "" {
		return ""
	}
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

func resourceOrPath(route merchant.Route, params map[string]string, eventID string) string {
	if res := route.ResourceFor(params, eventID); res != "" {
		return res
	}
	return route.Match.Path
}
