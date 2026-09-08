package publicapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
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
		Help: "Incoming allocation attempts divided by authorised executions forwarded.",
	}, []string{"resource"})
	downstreamEAF = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "bruiser_downstream_eaf",
		Help: "Authorised executions forwarded divided by distinct customers who attempted.",
	}, []string{"resource"})
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

type Server struct {
	cfg     config.Config
	store   *pgstore.Store
	leases  lease.Store
	signer  auth.Signer
	log     *slog.Logger
	profile merchant.Profile
	eaf     *eafAcc
	mux     http.Handler
	limit   *limit.PerExecution
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
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Get("/healthz", s.healthz)
	r.Get("/readyz", s.readyz)
	r.Handle("/metrics", promhttp.Handler())
	r.Get("/.well-known/bruiser/jwks.json", s.jwks)
	r.Get("/.well-known/bruiser/protocol", s.protocol)
	r.Route("/v1", func(r chi.Router) {
		r.Post("/sessions", s.createSession)
		r.Post("/authorize", s.authorize)
		r.Post("/introspect", s.introspect)
		r.Group(func(r chi.Router) {
			r.Use(s.sessionAuth)
			r.Post("/executions/acquire", s.acquire)
			r.Post("/executions/{id}/renew", s.renew)
			r.Post("/executions/{id}/release", s.release)
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
	if err := s.store.Ping(ctx); err != nil {
		storeUnavailable.Inc()
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "error": "store_unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) jwks(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, auth.JWKS(s.signer.KID, s.signer.Public))
}

func (s *Server) protocol(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"protocol":     "bruiser",
		"version":      "0.1-draft",
		"capabilities": []string{"IDENTITY", "ACQUIRE", "RENEW", "RELEASE", "WATCH", "AUTHORIZE", "INTROSPECT"},
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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
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
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Resource == "" {
		writeErr(w, http.StatusBadRequest, "resource required")
		return
	}
	if body.Action == "" {
		body.Action = "purchase"
	}
	rule := lease.DefaultRuleName()
	domain := lease.DomainKey(sess.MerchantID, rule, sess.CustomerID, body.Resource)
	reqID := requestID(r)
	res, err := s.leases.Acquire(r.Context(), lease.AcquireRequest{
		MerchantID:  sess.MerchantID,
		DomainKey:   domain,
		CustomerID:  sess.CustomerID,
		Principal:   lease.Principal{Type: sess.PrincipalType, ID: sess.PrincipalID},
		SessionID:   sess.SessionID,
		Resource:    body.Resource,
		Action:      body.Action,
		RuleName:    rule,
		MaxActive:   s.cfg.MaxActive,
		TTL:         s.cfg.LeaseTTL,
		MaxLifetime: s.cfg.MaxLifetime,
		RequestID:   reqID,
	})
	if err != nil {
		s.storeError(w, err)
		return
	}
	acquireTotal.WithLabelValues(res.Status, rule).Inc()
	switch res.Status {
	case lease.StatusGranted:
		s.writeExecution(w, http.StatusCreated, res.Execution, true)
	case lease.StatusAlreadyHeld:
		s.writeExecution(w, http.StatusOK, res.Execution, true)
	case lease.StatusBusy:
		retry := time.Until(res.Busy.ExpiresAt)
		if retry < 0 {
			retry = time.Second
		}
		writeJSON(w, http.StatusConflict, map[string]any{
			"status":              "BUSY",
			"domain":              domain,
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
	if e.EndReason != "" {
		body["end_reason"] = e.EndReason
	}
	if includeToken && e.State == lease.StateActive {
		tok, err := s.signer.SignExecution(*e)
		if err == nil {
			body["execution_token"] = tok
		}
	}
	writeJSON(w, status, body)
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
