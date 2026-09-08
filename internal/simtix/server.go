// Package simtix is an in-memory box-office analogue for the Arsenal-like lab.
// It is not Arsenal's system and not Ticketmaster. Routes match configs/arsenal.yaml.
package simtix

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
)

const CookieName = "boxoffice_session"
const DefaultEvent = "ars-che"

var ErrStaleFence = errors.New("stale fence")

type Config struct {
	HMACSecret       string
	OriginSecret     string // empty = origin lockdown off
	Seats            int
	RequireExecution bool
	VerifyExecution  func(token string) error
}

type Server struct {
	cfg   Config
	mu    sync.Mutex
	event event
	holds map[string]hold
	seq   int
}

type event struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Seats int    `json:"seats"`
	Held  int    `json:"held"`
	Sold  int    `json:"sold"`
}

func (e event) Available() int { return e.Seats - e.Held - e.Sold }

type hold struct {
	ID           string    `json:"hold_id"`
	EventID      string    `json:"event_id"`
	MembershipNo string    `json:"membership_no"`
	Seats        int       `json:"seats"`
	ExpiresAt    time.Time `json:"expires_at"`
}

func New(cfg Config) *Server {
	if cfg.HMACSecret == "" {
		cfg.HMACSecret = env("BRUISER_DEV_HMAC_SECRET", "dev-secret-change-me")
	}
	if cfg.Seats <= 0 {
		cfg.Seats = 50
	}
	return &Server{
		cfg: cfg,
		event: event{
			ID:    DefaultEvent,
			Name:  "Arsenal v Chelsea",
			Seats: cfg.Seats,
		},
		holds: map[string]hold{},
	}
}

func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "simtix"})
	})
	r.Post("/login", s.login)
	r.Get("/api/events", s.listEvents)
	r.Get("/api/events/{event}", s.getEvent)
	r.Post("/api/events/{event}/holds", s.createHold)
	r.Post("/api/orders", s.createOrder)
	r.Post("/reset", s.reset)
	return r
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MembershipNo string `json:"membership_no"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.MembershipNo == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "membership_no required"})
		return
	}
	tok, err := auth.IssueBoxOfficeSession(s.cfg.HMACSecret, body.MembershipNo, time.Hour)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "session"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]string{
		"membership_no": body.MembershipNo,
		"cookie":        CookieName,
	})
}

func (s *Server) listEvents(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireHoldsLocked(time.Now())
	ev := s.event
	writeJSON(w, http.StatusOK, map[string]any{
		"events": []map[string]any{{
			"id":        ev.ID,
			"name":      ev.Name,
			"available": ev.Available(),
		}},
	})
}

func (s *Server) getEvent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "event")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireHoldsLocked(time.Now())
	if id != s.event.ID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown event"})
		return
	}
	ev := s.event
	writeJSON(w, http.StatusOK, map[string]any{
		"id":        ev.ID,
		"name":      ev.Name,
		"available": ev.Available(),
	})
}

func (s *Server) createHold(w http.ResponseWriter, r *http.Request) {
	if !s.executionOK(w, r) {
		return
	}
	if !s.originOK(w, r, true) {
		return
	}
	eventID := chi.URLParam(r, "event")
	var body struct {
		Seats int `json:"seats"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Seats < 1 {
		body.Seats = 1
	}
	membership := membershipFrom(r, s.cfg.HMACSecret)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireHoldsLocked(time.Now())
	if eventID != s.event.ID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown event"})
		return
	}
	if s.event.Available() < body.Seats {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "sold out"})
		return
	}
	s.seq++
	h := hold{
		ID:           "hld_" + strconv.Itoa(s.seq),
		EventID:      eventID,
		MembershipNo: membership,
		Seats:        body.Seats,
		ExpiresAt:    time.Now().UTC().Add(120 * time.Second),
	}
	s.holds[h.ID] = h
	s.event.Held += body.Seats
	writeJSON(w, http.StatusCreated, h)
}

func (s *Server) createOrder(w http.ResponseWriter, r *http.Request) {
	if !s.executionOK(w, r) {
		return
	}
	if !s.originOK(w, r, true) {
		return
	}
	var body struct {
		EventID string `json:"event_id"`
		HoldID  string `json:"hold_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireHoldsLocked(time.Now())
	h, ok := s.holds[body.HoldID]
	if !ok || (body.EventID != "" && h.EventID != body.EventID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "hold not found"})
		return
	}
	delete(s.holds, h.ID)
	s.event.Held -= h.Seats
	s.event.Sold += h.Seats
	writeJSON(w, http.StatusCreated, map[string]any{
		"order_id": "ord_" + strconv.Itoa(s.seq),
		"event_id": h.EventID,
		"seats":    h.Seats,
		"customer": h.MembershipNo,
	})
}

func (s *Server) reset(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.event.Held = 0
	s.event.Sold = 0
	s.holds = map[string]hold{}
	s.seq = 0
	writeJSON(w, http.StatusOK, map[string]string{"status": "reset"})
}

func (s *Server) executionOK(w http.ResponseWriter, r *http.Request) bool {
	if !s.cfg.RequireExecution {
		return true
	}
	tok := r.Header.Get("X-Bruiser-Execution")
	if tok == "" {
		h := r.Header.Get("Authorization")
		if len(h) > 7 && (h[:7] == "Bearer " || h[:7] == "bearer ") {
			tok = h[7:]
		}
	}
	if tok == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "missing execution token"})
		return false
	}
	if s.cfg.VerifyExecution == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "execution verifier not configured"})
		return false
	}
	if err := s.cfg.VerifyExecution(tok); err != nil {
		if errors.Is(err, ErrStaleFence) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "stale fence"})
			return false
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid execution token"})
		return false
	}
	return true
}

func (s *Server) originOK(w http.ResponseWriter, r *http.Request, allocation bool) bool {
	if !allocation || s.cfg.OriginSecret == "" {
		return true
	}
	got := r.Header.Get("X-Bruiser-Origin-Secret")
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.OriginSecret)) != 1 {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error":  "origin lockdown",
			"detail": "allocation requires X-Bruiser-Origin-Secret from the enforcement front",
		})
		return false
	}
	return true
}

func (s *Server) expireHoldsLocked(now time.Time) {
	for id, h := range s.holds {
		if !h.ExpiresAt.After(now) {
			s.event.Held -= h.Seats
			if s.event.Held < 0 {
				s.event.Held = 0
			}
			delete(s.holds, id)
		}
	}
}

func membershipFrom(r *http.Request, secret string) string {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	a, err := auth.ParseAssertionHS256(c.Value, secret, "bruiser")
	if err != nil {
		return ""
	}
	return a.CustomerID
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
