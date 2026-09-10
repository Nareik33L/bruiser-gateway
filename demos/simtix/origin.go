package main

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/Nareik33L/bruiser-gateway/demos/seed"
	"github.com/Nareik33L/bruiser-gateway/demos/shared"
)

type originConfig struct {
	HMACSecret   string
	SSOSecret    string
	OriginSecret string
	AdminSecret  string
	Opponent     string
	PublicURL    string
}

type origin struct {
	cfg    originConfig
	mu     sync.Mutex
	events map[string]*liveEvent
	holds  map[string]hold
	orders map[string]order
	log    []reqLog
	seq    int
}

type liveEvent struct {
	seed.Event
	Blocks []seed.Block
	Held   int
	Sold   int
}

func (e *liveEvent) Available() int { return e.Seats - e.Held - e.Sold }

type hold struct {
	ID           string    `json:"hold_id"`
	EventID      string    `json:"event_id"`
	MembershipNo string    `json:"membership_no"`
	BlockID      string    `json:"block_id"`
	Seats        int       `json:"seats"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type order struct {
	ID           string    `json:"order_id"`
	EventID      string    `json:"event_id"`
	MembershipNo string    `json:"membership_no"`
	HoldID       string    `json:"hold_id"`
	Seats        int       `json:"seats"`
	CreatedAt    time.Time `json:"created_at"`
}

type reqLog struct {
	At           time.Time `json:"at"`
	Method       string    `json:"method"`
	Path         string    `json:"path"`
	Status       int       `json:"status"`
	MembershipNo string    `json:"membership_no,omitempty"`
	Decision     string    `json:"decision,omitempty"`
}

func newOrigin(cfg originConfig) *origin {
	o := &origin{
		cfg:    cfg,
		events: map[string]*liveEvent{},
		holds:  map[string]hold{},
		orders: map[string]order{},
	}
	for _, ev := range seed.Events(cfg.Opponent) {
		le := &liveEvent{Event: ev, Blocks: seed.Blocks(ev.ID)}
		if ev.ID != seed.HeadlineEventID {
			le.Blocks = nil
		}
		o.events[ev.ID] = le
	}
	return o
}

func (o *origin) handler() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		shared.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "simtix"})
	})
	r.Get("/assets/simtix.css", o.css)
	r.Get("/", o.pageHome)
	r.Get("/sso", o.sso)
	r.Get("/events/{event}", o.pageEvent)
	r.Get("/events/{event}/seats", o.pageSeats)
	r.Get("/basket", o.pageBasket)
	r.Get("/checkout", o.pageCheckout)
	r.Post("/checkout", o.postCheckout)
	r.Get("/confirmation/{order}", o.pageConfirm)

	r.Get("/api/events", o.listEvents)
	r.Get("/api/events/{event}", o.getEvent)
	r.Post("/api/events/{event}/holds", o.createHold)
	r.Post("/api/orders", o.createOrder)
	r.Post("/api/holds/{hold}/release", o.releaseHold)

	r.Get("/_admin/stats", o.adminStats)
	r.Post("/_admin/reset", o.adminReset)
	r.Post("/_admin/clear-logs", o.adminClearLogs)
	r.Get("/_internal/orders", o.internalOrders)
	return r
}

func (o *origin) sso(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("token")
	if tok == "" {
		http.Error(w, "missing handoff token", http.StatusUnauthorized)
		return
	}
	c, err := shared.Parse(tok, o.cfg.SSOSecret, "simtix")
	if err != nil {
		http.Error(w, "invalid handoff token", http.StatusUnauthorized)
		return
	}
	cookie, err := shared.IssueBoxOffice(o.cfg.HMACSecret, c.Subject, time.Hour)
	if err != nil {
		http.Error(w, "session", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     shared.BoxOfficeCookie,
		Value:    cookie,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     "simtix_name",
		Value:    c.Name,
		Path:     "/",
		SameSite: http.SameSiteLaxMode,
	})
	next := "/events/" + seed.HeadlineEventID
	if !c.Eligible {
		next += "?ineligible=1"
	}
	http.Redirect(w, r, next, http.StatusFound)
}

func (o *origin) listEvents(w http.ResponseWriter, _ *http.Request) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.expireLocked(time.Now())
	var events []map[string]any
	for _, ev := range o.events {
		events = append(events, map[string]any{
			"id": ev.ID, "name": ev.Name, "available": ev.Available(), "on_sale": ev.OnSale,
		})
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"events": events})
}

func (o *origin) getEvent(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "event")
	o.mu.Lock()
	defer o.mu.Unlock()
	o.expireLocked(time.Now())
	ev, ok := o.events[id]
	if !ok {
		shared.WriteErr(w, http.StatusNotFound, "unknown event")
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{
		"id": ev.ID, "name": ev.Name, "available": ev.Available(), "held": ev.Held, "sold": ev.Sold, "seats": ev.Seats,
	})
}

func (o *origin) createHold(w http.ResponseWriter, r *http.Request) {
	if !o.originOK(w, r) {
		return
	}
	eventID := chi.URLParam(r, "event")
	var body struct {
		Seats   int    `json:"seats"`
		BlockID string `json:"block_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Seats < 1 {
		body.Seats = 1
	}
	membership := membershipFrom(r, o.cfg.HMACSecret)
	o.mu.Lock()
	defer o.mu.Unlock()
	o.expireLocked(time.Now())
	ev, ok := o.events[eventID]
	if !ok {
		shared.WriteErr(w, http.StatusNotFound, "unknown event")
		return
	}
	if !ev.OnSale {
		shared.WriteErr(w, http.StatusConflict, "not on sale")
		return
	}
	if membership != "" {
		sup, ok := namedEligible(membership)
		if ok && !sup {
			shared.WriteJSON(w, http.StatusForbidden, map[string]string{
				"error":  "not eligible",
				"detail": "This members' sale is not available on your membership.",
			})
			return
		}
		held := 0
		for _, h := range o.holds {
			if h.MembershipNo == membership && h.EventID == eventID {
				held += h.Seats
			}
		}
		for _, ord := range o.orders {
			if ord.MembershipNo == membership && ord.EventID == eventID {
				held += ord.Seats
			}
		}
		if held+body.Seats > seed.PerAccountLimit {
			shared.WriteErr(w, http.StatusConflict, "per-account limit")
			return
		}
	}
	if ev.Available() < body.Seats {
		shared.WriteErr(w, http.StatusConflict, "sold out")
		return
	}
	if body.BlockID == "" {
		body.BlockID = "best"
	}
	o.seq++
	h := hold{
		ID:           "hld_" + strconv.Itoa(o.seq),
		EventID:      eventID,
		MembershipNo: membership,
		BlockID:      body.BlockID,
		Seats:        body.Seats,
		ExpiresAt:    time.Now().UTC().Add(120 * time.Second),
	}
	o.holds[h.ID] = h
	ev.Held += body.Seats
	o.recordLocked(r, http.StatusCreated, membership, "ALLOW")
	shared.WriteJSON(w, http.StatusCreated, h)
}

func (o *origin) createOrder(w http.ResponseWriter, r *http.Request) {
	if !o.originOK(w, r) {
		return
	}
	var body struct {
		EventID string `json:"event_id"`
		HoldID  string `json:"hold_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		shared.WriteErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.expireLocked(time.Now())
	h, ok := o.holds[body.HoldID]
	if !ok || (body.EventID != "" && h.EventID != body.EventID) {
		shared.WriteErr(w, http.StatusNotFound, "hold not found")
		return
	}
	delete(o.holds, h.ID)
	if ev := o.events[h.EventID]; ev != nil {
		ev.Held -= h.Seats
		if ev.Held < 0 {
			ev.Held = 0
		}
		ev.Sold += h.Seats
	}
	o.seq++
	ord := order{
		ID: "ord_" + strconv.Itoa(o.seq), EventID: h.EventID, MembershipNo: h.MembershipNo,
		HoldID: h.ID, Seats: h.Seats, CreatedAt: time.Now().UTC(),
	}
	o.orders[ord.ID] = ord
	shared.WriteJSON(w, http.StatusCreated, ord)
}

func (o *origin) releaseHold(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "hold")
	o.mu.Lock()
	defer o.mu.Unlock()
	h, ok := o.holds[id]
	if !ok {
		shared.WriteErr(w, http.StatusNotFound, "hold not found")
		return
	}
	delete(o.holds, id)
	if ev := o.events[h.EventID]; ev != nil {
		ev.Held -= h.Seats
		if ev.Held < 0 {
			ev.Held = 0
		}
	}
	shared.WriteJSON(w, http.StatusOK, map[string]string{"status": "released"})
}

func (o *origin) adminAuth(w http.ResponseWriter, r *http.Request) bool {
	got := r.Header.Get("X-Demo-Admin-Secret")
	if subtle.ConstantTimeCompare([]byte(got), []byte(o.cfg.AdminSecret)) != 1 {
		shared.WriteErr(w, http.StatusUnauthorized, "bad admin secret")
		return false
	}
	return true
}

func (o *origin) adminStats(w http.ResponseWriter, r *http.Request) {
	if !o.adminAuth(w, r) {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.expireLocked(time.Now())
	ev := o.events[seed.HeadlineEventID]
	shared.WriteJSON(w, http.StatusOK, map[string]any{
		"event_id": ev.ID, "name": ev.Name, "seats": ev.Seats,
		"held": ev.Held, "sold": ev.Sold, "available": ev.Available(),
		"holds": len(o.holds), "orders": len(o.orders), "request_log": len(o.log),
	})
}

func (o *origin) adminReset(w http.ResponseWriter, r *http.Request) {
	if !o.adminAuth(w, r) {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.holds = map[string]hold{}
	o.orders = map[string]order{}
	o.log = nil
	o.seq = 0
	for _, ev := range o.events {
		ev.Held = 0
		ev.Sold = 0
	}
	ev := o.events[seed.HeadlineEventID]
	available := 0
	seats := seed.HeadlineSeats
	if ev != nil {
		available = ev.Available()
		seats = ev.Seats
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{
		"status":    "reset",
		"seats":     seats,
		"available": available,
		"sold":      0,
		"held":      0,
	})
}

func (o *origin) adminClearLogs(w http.ResponseWriter, r *http.Request) {
	if !o.adminAuth(w, r) {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	o.log = nil
	shared.WriteJSON(w, http.StatusOK, map[string]string{"status": "cleared"})
}

func (o *origin) internalOrders(w http.ResponseWriter, r *http.Request) {
	got := r.Header.Get("X-Harchester-SSO-Secret")
	if subtle.ConstantTimeCompare([]byte(got), []byte(o.cfg.SSOSecret)) != 1 {
		shared.WriteErr(w, http.StatusUnauthorized, "forbidden")
		return
	}
	mem := r.URL.Query().Get("membership")
	o.mu.Lock()
	defer o.mu.Unlock()
	var out []order
	for _, ord := range o.orders {
		if ord.MembershipNo == mem {
			out = append(out, ord)
		}
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"orders": out})
}

func (o *origin) originOK(w http.ResponseWriter, r *http.Request) bool {
	if o.cfg.OriginSecret == "" {
		return true
	}
	got := r.Header.Get("X-Bruiser-Origin-Secret")
	if subtle.ConstantTimeCompare([]byte(got), []byte(o.cfg.OriginSecret)) != 1 {
		shared.WriteJSON(w, http.StatusForbidden, map[string]string{
			"error":  "origin lockdown",
			"detail": "allocation requires X-Bruiser-Origin-Secret from the Edge",
		})
		return false
	}
	return true
}

func (o *origin) expireLocked(now time.Time) {
	for id, h := range o.holds {
		if !h.ExpiresAt.After(now) {
			if ev := o.events[h.EventID]; ev != nil {
				ev.Held -= h.Seats
				if ev.Held < 0 {
					ev.Held = 0
				}
			}
			delete(o.holds, id)
		}
	}
}

func (o *origin) recordLocked(r *http.Request, status int, membership, decision string) {
	o.log = append(o.log, reqLog{
		At: time.Now().UTC(), Method: r.Method, Path: r.URL.Path,
		Status: status, MembershipNo: membership, Decision: decision,
	})
	if len(o.log) > 5000 {
		o.log = o.log[len(o.log)-4000:]
	}
}

func membershipFrom(r *http.Request, secret string) string {
	c, err := r.Cookie(shared.BoxOfficeCookie)
	if err != nil {
		return ""
	}
	cl, err := shared.Parse(c.Value, secret, "bruiser")
	if err != nil {
		return ""
	}
	return cl.Subject
}

func namedEligible(membership string) (eligible bool, known bool) {
	switch membership {
	case seed.AliceMembership:
		return true, true
	case seed.IneligibleMember:
		return false, true
	default:
		return true, false
	}
}
