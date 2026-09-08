package publicapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/golang-jwt/jwt/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/config"
	"github.com/Nareik33L/bruiser-gateway/internal/id"
	"github.com/Nareik33L/bruiser-gateway/internal/lease"
	"github.com/Nareik33L/bruiser-gateway/internal/limit"
	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
	"github.com/Nareik33L/bruiser-gateway/internal/ops"
	"github.com/Nareik33L/bruiser-gateway/internal/policy"
	pgstore "github.com/Nareik33L/bruiser-gateway/internal/store/postgres"
)

var (
	acquireTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "bruiser_acquire_total",
		Help: "Acquire outcomes.",
	}, []string{"outcome", "rule"})
	storeUnavailable = promauto.NewCounter(prometheus.CounterOpts{
		Name: "bruiser_store_unavailable",
		Help: "Store unavailability events.",
	})
	allocationAttempts = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "bruiser_allocation_attempts_total",
		Help: "Incoming scarce-inventory requests Bruiser saw.",
	}, []string{"resource", "outcome"})
	executionsForwarded = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "bruiser_executions_forwarded_total",
		Help: "Authorised allocation requests forwarded toward origin.",
	}, []string{"resource"})
	observedEAF = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "bruiser_observed_eaf",
		Help: "Process-local incoming allocation attempts ÷ authorised executions forwarded. Do not sum this gauge across replicas. Cluster EAF = sum(allocation_attempts_total) / sum(executions_forwarded_total).",
	}, []string{"resource"})
	downstreamEAF = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "bruiser_downstream_eaf",
		Help: "Process-local authorised executions forwarded ÷ distinct customers this process saw. Do not sum across replicas.",
	}, []string{"resource"})
	handoffTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "bruiser_handoff_total",
		Help: "Handoff operations by mode.",
	}, []string{"mode"})
	revokeTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "bruiser_revoke_total",
		Help: "Customer or admin revokes.",
	})
	queueDepth = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "bruiser_queue_depth",
		Help: "Live intra-customer waiters.",
	}, []string{"merchant"})
)

type eafAcc struct {
	mu        sync.Mutex
	attempts  map[string]float64
	forwarded map[string]float64
	customers map[string]map[string]struct{}
}

func newEAFAcc() *eafAcc {
	return &eafAcc{
		attempts:  map[string]float64{},
		forwarded: map[string]float64{},
		customers: map[string]map[string]struct{}{},
	}
}

func (a *eafAcc) record(resource, customer, outcome string) {
	if a == nil || resource == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.attempts[resource]++
	allocationAttempts.WithLabelValues(resource, outcome).Inc()
	if outcome == "allow" {
		a.forwarded[resource]++
		executionsForwarded.WithLabelValues(resource).Inc()
	}
	if customer != "" {
		if a.customers[resource] == nil {
			a.customers[resource] = map[string]struct{}{}
		}
		a.customers[resource][customer] = struct{}{}
	}
	att := a.attempts[resource]
	fwd := a.forwarded[resource]
	if fwd > 0 {
		observedEAF.WithLabelValues(resource).Set(att / fwd)
	}
	n := float64(len(a.customers[resource]))
	if n > 0 {
		downstreamEAF.WithLabelValues(resource).Set(fwd / n)
	}
}

func (a *eafAcc) snapshot(resource string) (attempts, forwarded, customers float64) {
	if a == nil {
		return 0, 0, 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.attempts[resource], a.forwarded[resource], float64(len(a.customers[resource]))
}

func (a *eafAcc) snapshotAll() []map[string]any {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]map[string]any, 0, len(a.attempts))
	for res, att := range a.attempts {
		fwd := a.forwarded[res]
		cust := float64(len(a.customers[res]))
		obs, down := 0.0, 0.0
		if fwd > 0 {
			obs = att / fwd
		}
		if cust > 0 {
			down = fwd / cust
		}
		out = append(out, map[string]any{
			"resource":       res,
			"attempts":       att,
			"forwarded":      fwd,
			"customers":      cust,
			"observed_eaf":   obs,
			"downstream_eaf": down,
		})
	}
	return out
}

func (a *eafAcc) headline() (observed, downstream, attempts, forwarded float64) {
	if a == nil {
		return 0, 0, 0, 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for res, att := range a.attempts {
		attempts += att
		forwarded += a.forwarded[res]
	}
	seen := map[string]struct{}{}
	if forwarded > 0 {
		observed = attempts / forwarded
	}
	for res := range a.customers {
		for c := range a.customers[res] {
			seen[c] = struct{}{}
		}
	}
	if n := float64(len(seen)); n > 0 && forwarded > 0 {
		downstream = forwarded / n
	}
	return observed, downstream, attempts, forwarded
}

type Server struct {
	cfg       config.Config
	store     *pgstore.Store
	leases    lease.Store
	signer    auth.Signer
	log       *slog.Logger
	profile   merchant.Profile
	eaf       *eafAcc
	mux       http.Handler
	limit     *limit.PerExecution
	compiled  atomic.Pointer[policy.Compiled]
	policyVer atomic.Int64
	stopWatch context.CancelFunc
	controls  atomic.Pointer[ops.Controls]
}

func New(cfg config.Config, store *pgstore.Store, signer auth.Signer, log *slog.Logger, profile merchant.Profile) *Server {
	s := &Server{
		cfg:     cfg,
		store:   store,
		leases:  lease.NewBusyCache(store, 50),
		signer:  signer,
		log:     log,
		profile: profile,
		eaf:     newEAFAcc(),
		limit:   limit.New(cfg.MaxInFlight, cfg.RatePerSec),
	}
	if cfg.Burst > 0 {
		s.limit.Burst = cfg.Burst
	}
	s.initControls()
	s.loadPolicy()
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(requestDeadline(8 * time.Second))
	r.Get("/healthz", s.healthz)
	r.Get("/readyz", s.readyz)
	r.Handle("/metrics", promhttp.Handler())
	r.Get("/admin", s.adminPage)
	r.Post("/admin", s.adminPage)
	r.Get("/demo", s.adminPage)
	r.Get("/.well-known/bruiser/jwks.json", s.jwks)
	r.Get("/.well-known/bruiser/protocol", s.protocol)
	r.Route("/v1", func(r chi.Router) {
		r.Post("/sessions", s.createSession)
		r.Post("/authorize", s.authorize)
		r.Post("/introspect", s.introspect)
		r.Get("/policy", s.getPolicy)
		r.Put("/policy", s.putPolicy)
		r.Get("/policy/history", s.policyHistory)
		r.Get("/admin/status", s.adminStatus)
		r.Get("/admin/stream", s.adminStream)
		r.Get("/admin/audit", s.adminAudit)
		r.Get("/admin/export", s.adminExport)
		r.Get("/admin/customer/{id}", s.adminCustomer)
		r.Post("/admin/authority-check", s.adminRunCheck)
		r.Get("/admin/authority-check", s.adminLastCheck)
		r.Post("/admin/executions/{id}/revoke", s.adminRevoke)
		r.Get("/admin/controls", s.adminGetControls)
		r.Put("/admin/controls", s.adminPutControls)
		r.Post("/admin/controls/drain", s.adminDrain)
		r.Post("/admin/controls/revoke-all", s.adminRevokeAll)
		r.Get("/admin/dry-run", s.adminDryRun)
		r.Get("/admin/ramp", s.adminDryRun)
		r.Post("/authority-check", s.adminRunCheck)
		r.Get("/authority-check", s.adminLastCheck)
		r.Group(func(r chi.Router) {
			r.Use(s.sessionAuth)
			r.Post("/executions/acquire", s.acquire)
			r.Post("/executions/{id}/renew", s.renew)
			r.Post("/executions/{id}/heartbeat", s.renew)
			r.Post("/executions/{id}/release", s.release)
			r.Post("/executions/{id}/leave", s.release)
			r.Post("/executions/{id}/handoff", s.handoff)
			r.Post("/executions/{id}/revoke", s.revoke)
			r.Get("/executions/{id}", s.get)
			r.Get("/executions/{id}/watch", s.watch)
		})
	})
	s.mux = r
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) EAF(resource string) (attempts, forwarded, customers float64) {
	return s.eaf.snapshot(resource)
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.ReadyTimeout)
	defer cancel()
	ctl := s.currentControls()
	body := map[string]any{
		"status":          "ready",
		"store":           "ok",
		"mode":            ctl.Mode,
		"enforcement":     ctl.Enforcement,
		"enforce_percent": ctl.EffectivePercent(),
		"signing_key":     "unknown",
		"upstream":        "skipped",
		"admin_secret":    "ok",
	}
	if err := s.cfg.ValidateSecrets(); err != nil {
		body["status"] = "not_ready"
		body["admin_secret"] = "invalid"
		body["config"] = err.Error()
		writeJSON(w, http.StatusServiceUnavailable, body)
		return
	}
	if err := s.store.Ping(ctx); err != nil {
		storeUnavailable.Inc()
		body["status"] = "not_ready"
		body["store"] = "unavailable"
		writeJSON(w, http.StatusServiceUnavailable, body)
		return
	}
	if keys, err := s.store.ActiveSigningKeys(ctx, s.cfg.MerchantID); err != nil {
		body["signing_key"] = "error"
	} else if len(keys) == 0 {
		body["signing_key"] = "missing"
		body["status"] = "not_ready"
		writeJSON(w, http.StatusServiceUnavailable, body)
		return
	} else {
		body["signing_key"] = "ok"
	}
	if s.cfg.OriginURL != "" {
		u := strings.TrimRight(s.cfg.OriginURL, "/") + "/healthz"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			body["upstream"] = "error"
		} else if resp, err := http.DefaultClient.Do(req); err != nil {
			body["upstream"] = "unreachable"
		} else {
			resp.Body.Close()
			body["upstream"] = "ok"
		}
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) jwks(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, auth.JWKS(s.signer.KID, s.signer.Public))
}

func (s *Server) protocol(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"protocol":     "bruiser",
		"version":      "0.1-draft",
		"capabilities": []string{"IDENTITY", "ACQUIRE", "RENEW", "HEARTBEAT", "RELEASE", "HANDOFF", "REVOKE", "WATCH", "QUEUE", "AUTHORIZE", "INTROSPECT", "POLICY", "ADMIN", "AUTHORITY_CHECK", "DRY_RUN", "RAMP", "CONTROLS"},
	})
}

type sessionReq struct {
	Principal struct {
		Type   string `json:"type"`
		ID     string `json:"id"`
		Vendor string `json:"vendor"`
	} `json:"principal"`
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	raw := bearer(r)
	if raw == "" {
		writeErr(w, http.StatusUnauthorized, "missing customer assertion")
		return
	}
	assertion, err := auth.ParseAssertionHS256(raw, s.cfg.DevHMACSecret, "bruiser")
	if err != nil {
		writeErr(w, http.StatusUnauthorized, "invalid customer assertion")
		return
	}
	var body sessionReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Principal.Type == "" {
		body.Principal.Type = "agent"
	}
	if body.Principal.Type != "agent" && body.Principal.Type != "browser" {
		writeErr(w, http.StatusForbidden, "principal type not allowed")
		return
	}
	if body.Principal.ID == "" {
		writeErr(w, http.StatusBadRequest, "principal.id required")
		return
	}
	sessID := id.Session()
	exp := time.Now().UTC().Add(s.cfg.SessionTTL)
	if err := s.store.InsertSession(r.Context(), pgstore.SessionRow{
		ID:            sessID,
		MerchantID:    s.cfg.MerchantID,
		CustomerID:    assertion.CustomerID,
		Anchors:       assertion.Anchors,
		PrincipalType: body.Principal.Type,
		PrincipalID:   body.Principal.ID,
		ExpiresAt:     exp,
	}); err != nil {
		s.storeError(w, err)
		return
	}
	token, err := s.signer.SignSession(auth.SessionClaims{
		MerchantID:    s.cfg.MerchantID,
		CustomerID:    assertion.CustomerID,
		PrincipalType: body.Principal.Type,
		PrincipalID:   body.Principal.ID,
		SessionID:     sessID,
		Anchors:       assertion.Anchors,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(exp),
			IssuedAt:  jwt.NewNumericDate(time.Now().UTC()),
			Subject:   assertion.CustomerID,
		},
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "sign session")
		return
	}
	anchorKeys := make([]string, 0, len(assertion.Anchors))
	for k := range assertion.Anchors {
		anchorKeys = append(anchorKeys, k)
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"session_id":    sessID,
		"session_token": token,
		"expires_at":    exp.Format(time.RFC3339Nano),
		"customer_id":   assertion.CustomerID,
		"anchors":       anchorKeys,
	})
}

type ctxKey int

const sessionKey ctxKey = 1

func (s *Server) sessionAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := bearer(r)
		if raw == "" {
			writeErr(w, http.StatusUnauthorized, "missing session token")
			return
		}
		claims, err := auth.ParseSession(raw, s.signer.Public)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "invalid session token")
			return
		}
		if claims.MerchantID != s.cfg.MerchantID {
			writeErr(w, http.StatusUnauthorized, "wrong merchant")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey, claims)))
	})
}

func sessionFrom(ctx context.Context) auth.SessionClaims {
	v, _ := ctx.Value(sessionKey).(auth.SessionClaims)
	return v
}

type acquireReq struct {
	Resource string `json:"resource"`
	Action   string `json:"action"`
}

func (s *Server) acquire(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r.Context())
	var body acquireReq
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Resource == "" {
		writeErr(w, http.StatusBadRequest, "resource required")
		return
	}
	if body.Action == "" {
		body.Action = "purchase"
	}
	d := s.evaluate(sess.MerchantID, sess.CustomerID, sess.PrincipalType, body.Resource, body.Action, sess.Anchors)
	d, ctl := s.applyControls(d, body.Action)
	if d.Denied || d.ControlNone {
		reason := d.Reason
		if reason == "" {
			reason = "denied"
		}
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "DENIED", "reason": reason, "rule_name": d.RuleName})
		return
	}
	ramp := s.rampInput(sess.CustomerID, body.Resource, body.Action, d.RuleName, "", "", sess.Anchors)
	if !ctl.ShouldEnforce(ramp) {
		s.writeAcquireDryRun(w, r, sess, body, d, ctl.EffectivePercent())
		return
	}
	reqID := requestID(r)
	res, err := s.leases.Acquire(r.Context(), lease.AcquireRequest{
		MerchantID:  sess.MerchantID,
		DomainKey:   d.DomainKey,
		CustomerID:  sess.CustomerID,
		Principal:   lease.Principal{Type: sess.PrincipalType, ID: sess.PrincipalID},
		SessionID:   sess.SessionID,
		Resource:    body.Resource,
		Action:      body.Action,
		RuleName:    d.RuleName,
		MaxActive:   d.MaxActive,
		TTL:         d.TTL,
		MaxLifetime: d.MaxLifetime,
		Precedence:  d.Precedence,
		MaxWaiters:  d.MaxWaiters,
		RequestID:   reqID,
	})
	if err != nil {
		if s.failOpen() {
			writeJSON(w, http.StatusOK, map[string]any{"status": "ALLOW", "control": "fail-open", "rule_name": d.RuleName})
			return
		}
		s.storeError(w, err)
		return
	}
	acquireTotal.WithLabelValues(res.Status, d.RuleName).Inc()
	s.recordAcquire(r.Context(), true, sess.CustomerID, sess.PrincipalID, body.Resource, body.Action, d.RuleName, res, reqID)
	w.Header().Set("X-Bruiser-Enforced", "1")
	w.Header().Set("X-Bruiser-Ramp", strconv.Itoa(ctl.EffectivePercent()))
	switch res.Status {
	case lease.StatusGranted:
		s.eaf.record(body.Resource, sess.CustomerID, "allow")
		s.writeExecution(w, http.StatusCreated, res.Execution, true)
	case lease.StatusAlreadyHeld:
		s.eaf.record(body.Resource, sess.CustomerID, "resume")
		s.writeExecution(w, http.StatusOK, res.Execution, true)
	case lease.StatusQueued:
		s.eaf.record(body.Resource, sess.CustomerID, "queued")
		s.writeQueued(w, d.DomainKey, d.RuleName, res)
	case lease.StatusBusy:
		s.eaf.record(body.Resource, sess.CustomerID, "busy")
		retry := time.Until(res.Busy.ExpiresAt)
		if retry < 0 {
			retry = time.Second
		}
		writeJSON(w, http.StatusConflict, map[string]any{
			"status":              "BUSY",
			"domain":              d.DomainKey,
			"rule_name":           d.RuleName,
			"active_execution_id": res.Busy.ActiveExecutionID,
			"holder":              map[string]string{"type": res.Busy.Holder.Type, "id": res.Busy.Holder.ID},
			"expires_at":          res.Busy.ExpiresAt.UTC().Format(time.RFC3339Nano),
			"retry_after_ms":      retry.Milliseconds(),
			"watch":               "/v1/executions/" + res.Busy.ActiveExecutionID + "/watch",
			"can_preempt":         res.Busy.CanPreempt,
		})
	default:
		writeErr(w, http.StatusForbidden, res.Reason)
	}
}

func (s *Server) writeAcquireDryRun(w http.ResponseWriter, r *http.Request, sess auth.SessionClaims, body acquireReq, d policy.Decision, percent int) {
	ev, err := s.store.ShadowDecide(r.Context(), sess.MerchantID, d.DomainKey, sess.CustomerID, sess.PrincipalID, body.Resource, body.Action, d.RuleName, d.MaxActive, d.MaxWaiters, d.TTL, requestID(r))
	would, reason := "WOULD_UNKNOWN", "store_unavailable"
	if err == nil {
		would, reason = "WOULD_"+ev.Would, ev.Reason
	}
	s.eaf.record(body.Resource, sess.CustomerID, "would_"+strings.ToLower(strings.TrimPrefix(would, "WOULD_")))
	w.Header().Set("X-Bruiser-Control", "observe")
	w.Header().Set("X-Bruiser-Dry-Run", would)
	w.Header().Set("X-Bruiser-Dry-Run-Reason", reason)
	w.Header().Set("X-Bruiser-Enforced", "0")
	w.Header().Set("X-Bruiser-Ramp", strconv.Itoa(percent))
	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "ALLOW",
		"would":           would,
		"reason":          reason,
		"rule_name":       d.RuleName,
		"customer_id":     sess.CustomerID,
		"enforced":        false,
		"enforce_percent": percent,
	})
}

func (s *Server) renew(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r.Context())
	exeID := chi.URLParam(r, "id")
	e, err := s.leases.Renew(r.Context(), sess.MerchantID, exeID, sess.SessionID, requestID(r), s.cfg.LeaseTTL)
	if err != nil {
		s.mutationError(w, err)
		return
	}
	s.writeExecution(w, http.StatusOK, &e, true)
}

func (s *Server) release(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r.Context())
	exeID := chi.URLParam(r, "id")
	e, err := s.leases.Release(r.Context(), sess.MerchantID, exeID, sess.SessionID, requestID(r))
	if err != nil {
		s.mutationError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"execution_id": e.ID,
		"state":        e.State,
		"end_reason":   e.EndReason,
	})
}

type handoffReq struct {
	To struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	} `json:"to"`
	Mode string `json:"mode"`
}

func (s *Server) handoff(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r.Context())
	var body handoffReq
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	}
	exeID := chi.URLParam(r, "id")
	var prec []string
	if e, err := s.leases.Get(r.Context(), sess.MerchantID, exeID); err == nil {
		prec = s.getCompiled().Precedence(e.RuleName)
	}
	res, err := s.leases.Handoff(r.Context(), lease.HandoffRequest{
		MerchantID:  sess.MerchantID,
		ExecutionID: exeID,
		SessionID:   sess.SessionID,
		To:          lease.Principal{Type: body.To.Type, ID: body.To.ID},
		Mode:        body.Mode,
		TTL:         s.cfg.LeaseTTL,
		Precedence:  prec,
		RequestID:   requestID(r),
	})
	if err != nil {
		s.mutationError(w, err)
		return
	}
	handoffTotal.WithLabelValues(res.Mode).Inc()
	s.writeExecution(w, http.StatusCreated, &res.Successor, true)
}

type revokeReq struct {
	Reason string `json:"reason"`
}

func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r.Context())
	var body revokeReq
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body)
	}
	e, err := s.leases.Revoke(r.Context(), sess.MerchantID, chi.URLParam(r, "id"), sess.SessionID, requestID(r), body.Reason)
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

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r.Context())
	e, err := s.leases.Get(r.Context(), sess.MerchantID, chi.URLParam(r, "id"))
	if err != nil {
		s.storeError(w, err)
		return
	}
	if e.CustomerID != sess.CustomerID {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	s.writeExecution(w, http.StatusOK, &e, false)
}

func (s *Server) watch(w http.ResponseWriter, r *http.Request) {
	sess := sessionFrom(r.Context())
	exeID := chi.URLParam(r, "id")
	initial, err := s.leases.Get(r.Context(), sess.MerchantID, exeID)
	if err != nil {
		s.storeError(w, err)
		return
	}
	if initial.CustomerID != sess.CustomerID {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	deadline := time.Now().Add(25 * time.Second)
	cur := initial
	for time.Now().Before(deadline) {
		select {
		case <-r.Context().Done():
			s.writeExecution(w, http.StatusOK, &cur, false)
			return
		case <-time.After(200 * time.Millisecond):
		}
		next, err := s.leases.Get(r.Context(), sess.MerchantID, exeID)
		if err != nil {
			break
		}
		if next.State != initial.State || !next.ExpiresAt.Equal(initial.ExpiresAt) {
			s.writeExecution(w, http.StatusOK, &next, false)
			return
		}
		cur = next
	}
	s.writeExecution(w, http.StatusOK, &cur, false)
}

func (s *Server) writeExecution(w http.ResponseWriter, status int, e *lease.Execution, includeToken bool) {
	body := map[string]any{
		"execution_id":    e.ID,
		"status":          e.State,
		"domain":          e.DomainKey,
		"resource":        e.Resource,
		"action":          e.Action,
		"customer_id":     e.CustomerID,
		"principal":       map[string]string{"type": e.Principal.Type, "id": e.Principal.ID},
		"fence":           e.Fence,
		"granted_at":      e.GrantedAt.UTC().Format(time.RFC3339Nano),
		"expires_at":      e.ExpiresAt.UTC().Format(time.RFC3339Nano),
		"max_lifetime_at": e.MaxLifetimeAt.UTC().Format(time.RFC3339Nano),
		"renew_count":     e.RenewCount,
		"rule_name":       e.RuleName,
	}
	if e.State == lease.StateActive {
		body["heartbeat_after_ms"] = s.cfg.HeartbeatInterval.Milliseconds()
	}
	if e.State == lease.StateQueued && e.QueuePosition > 0 {
		body["position"] = e.QueuePosition
	}
	if e.EndReason != "" {
		body["end_reason"] = e.EndReason
	}
	if e.SuccessorID != "" {
		body["successor_id"] = e.SuccessorID
	}
	if includeToken && e.State == lease.StateActive {
		tok, err := s.signer.SignExecution(*e)
		if err == nil {
			body["execution_token"] = tok
		}
	}
	writeJSON(w, status, body)
}

func (s *Server) writeQueued(w http.ResponseWriter, domain, rule string, res lease.AcquireResult) {
	retry := time.Until(res.Busy.ExpiresAt)
	if retry < 0 {
		retry = time.Second
	}
	body := map[string]any{
		"status":              "QUEUED",
		"domain":              domain,
		"rule_name":           rule,
		"execution_id":        res.Queue.WaiterID,
		"position":            res.Queue.Position,
		"active_execution_id": res.Queue.ActiveExecutionID,
		"expires_at":          res.Queue.ExpiresAt.UTC().Format(time.RFC3339Nano),
		"retry_after_ms":      retry.Milliseconds(),
		"watch":               "/v1/executions/" + res.Queue.WaiterID + "/watch",
	}
	if res.Busy != nil {
		body["holder"] = map[string]string{"type": res.Busy.Holder.Type, "id": res.Busy.Holder.ID}
	}
	writeJSON(w, http.StatusAccepted, body)
}

func (s *Server) mutationError(w http.ResponseWriter, err error) {
	var gone *lease.GoneError
	if errors.As(err, &gone) {
		writeJSON(w, http.StatusGone, map[string]any{
			"error":        "gone",
			"reason":       gone.Reason,
			"execution_id": gone.ExecutionID,
			"successor_id": gone.SuccessorID,
		})
		return
	}
	if errors.Is(err, lease.ErrNotHolder) {
		writeErr(w, http.StatusForbidden, "not holder")
		return
	}
	if errors.Is(err, lease.ErrPrecedence) {
		writeErr(w, http.StatusForbidden, "cannot preempt")
		return
	}
	s.storeError(w, err)
}

func (s *Server) storeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, lease.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not found")
	case errors.Is(err, lease.ErrInvalidInput):
		writeErr(w, http.StatusBadRequest, "invalid input")
	case errors.Is(err, lease.ErrUnavailable):
		storeUnavailable.Inc()
		w.Header().Set("Retry-After", "2")
		writeErr(w, http.StatusServiceUnavailable, "store unavailable")
	default:
		writeErr(w, http.StatusInternalServerError, "internal error")
	}
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func requestID(r *http.Request) string {
	if v := middleware.GetReqID(r.Context()); v != "" {
		return v
	}
	return id.Request()
}

func requestDeadline(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "/watch") || strings.HasSuffix(r.URL.Path, "/stream") {
				next.ServeHTTP(w, r)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return false
	}
	return true
}

func (s *Server) initControls() {
	c := ops.FromEnv(s.cfg.Mode, s.cfg.Enforcement, s.cfg.QueueEnabled, s.cfg.EnforcePercent)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if db, found, err := s.store.GetControls(ctx, s.cfg.MerchantID); err == nil && found {
		c = db
	}
	s.controls.Store(&c)
}

func (s *Server) currentControls() ops.Controls {
	if p := s.controls.Load(); p != nil {
		return *p
	}
	return ops.FromEnv(s.cfg.Mode, s.cfg.Enforcement, s.cfg.QueueEnabled, s.cfg.EnforcePercent)
}

func (s *Server) applyControls(d policy.Decision, action string) (policy.Decision, ops.Controls) {
	c := s.currentControls()
	d.MaxWaiters = c.EffectiveWaiters(d.MaxWaiters)
	if c.LeaseTTLSeconds > 0 {
		d.TTL = time.Duration(c.LeaseTTLSeconds) * time.Second
	}
	if c.ActionDisabled(action) {
		d.ControlNone = true
	}
	return d, c
}

func (s *Server) failOpen() bool {
	c := s.currentControls()
	return !c.FailClosed || c.PassThrough()
}

func (s *Server) rampInput(customer, resource, action, rule, event, path string, anchors map[string]string) ops.RampInput {
	in := ops.RampInput{
		CustomerID: customer,
		Resource:   resource,
		Action:     action,
		RuleName:   rule,
		EventID:    event,
		Path:       path,
		Env:        s.cfg.Environment,
	}
	if anchors != nil {
		in.Pool = anchors["pool"]
		in.Cohort = anchors["cohort"]
	}
	if in.EventID == "" {
		if i := strings.LastIndex(resource, ":"); i >= 0 && i+1 < len(resource) {
			in.EventID = resource[i+1:]
		}
	}
	return in
}

func (s *Server) recordDecision(ctx context.Context, enforced bool, customer, principal, resource, action, rule, would, reason, reqID, errText string) {
	_ = s.store.RecordDecision(ctx, s.cfg.MerchantID, pgstore.DryRunEvent{
		Would:       would,
		Reason:      reason,
		CustomerID:  customer,
		PrincipalID: principal,
		Resource:    resource,
		Action:      action,
		RuleName:    rule,
		RequestID:   reqID,
		Enforced:    enforced,
		Error:       errText,
	})
}

func (s *Server) recordAcquire(ctx context.Context, enforced bool, customer, principal, resource, action, rule string, acq lease.AcquireResult, reqID string) {
	would, reason := "REJECT", acq.Reason
	switch acq.Status {
	case lease.StatusGranted, lease.StatusAlreadyHeld:
		would, reason = "ALLOW", acq.Status
	case lease.StatusQueued:
		would, reason = "QUEUE", "queued"
	case lease.StatusBusy:
		would, reason = "REJECT", "busy"
	}
	s.recordDecision(ctx, enforced, customer, principal, resource, action, rule, would, reason, reqID, "")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
