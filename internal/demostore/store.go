// Package demostore is a Bruiser-hosted sandbox shop for the Attack Lab.
// It is not Shopify, Ticketmaster, or any third-party origin.
package demostore

import (
	"crypto/subtle"
	"encoding/json"
	"html"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/id"
)

const CookieName = "boxoffice_session"

type Config struct {
	HMACSecret   string
	OriginSecret string
}

type Store struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Product   string    `json:"product"`
	Price     string    `json:"price"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type Order struct {
	ID       string    `json:"order_id"`
	StoreID  string    `json:"store_id"`
	Customer string    `json:"customer"`
	Wave     string    `json:"wave,omitempty"`
	At       time.Time `json:"at"`
}

type Registry struct {
	cfg    Config
	mu     sync.Mutex
	stores map[string]*liveStore
}

type liveStore struct {
	Store
	orders []Order
	seq    int
}

func New(cfg Config) *Registry {
	return &Registry{
		cfg:    cfg,
		stores: map[string]*liveStore{},
	}
}

func (r *Registry) Create(name, product, price string, ttl time.Duration) Store {
	if ttl <= 0 {
		ttl = 20 * time.Minute
	}
	now := time.Now().UTC()
	st := Store{
		ID:        id.New("str"),
		Name:      name,
		Product:   product,
		Price:     price,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}
	r.mu.Lock()
	r.stores[st.ID] = &liveStore{Store: st}
	r.mu.Unlock()
	return st
}

func (r *Registry) Get(id string) (Store, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ls, ok := r.stores[id]
	if !ok || !ls.ExpiresAt.After(time.Now()) {
		if ok {
			delete(r.stores, id)
		}
		return Store{}, false
	}
	return ls.Store, true
}

func (r *Registry) Delete(id string) {
	r.mu.Lock()
	delete(r.stores, id)
	r.mu.Unlock()
}

func (r *Registry) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	now := time.Now()
	for id, ls := range r.stores {
		if !ls.ExpiresAt.After(now) {
			delete(r.stores, id)
			continue
		}
		n++
	}
	return n
}

func (r *Registry) ExpireDue() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	now := time.Now()
	for id, ls := range r.stores {
		if !ls.ExpiresAt.After(now) {
			delete(r.stores, id)
			n++
		}
	}
	return n
}

func (r *Registry) Orders(storeID, wave string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	ls, ok := r.stores[storeID]
	if !ok {
		return 0
	}
	if wave == "" {
		return len(ls.orders)
	}
	n := 0
	for _, o := range ls.orders {
		if o.Wave == wave {
			n++
		}
	}
	return n
}

func (r *Registry) Handler() http.Handler {
	mux := chi.NewRouter()
	mux.Get("/s/{store}", r.productPage)
	mux.Get("/s/{store}/product", r.productJSON)
	mux.Post("/s/{store}/checkout", r.checkout)
	mux.Post("/s/{store}/drops/{wave}/checkout", r.checkout)
	return mux
}

func (r *Registry) productPage(w http.ResponseWriter, req *http.Request) {
	st, ok := r.Get(chi.URLParam(req, "store"))
	if !ok {
		http.Error(w, "store expired", http.StatusNotFound)
		return
	}
	r.maybeIssueFanCookie(w, req)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(productHTML(st)))
}

func (r *Registry) productJSON(w http.ResponseWriter, req *http.Request) {
	st, ok := r.Get(chi.URLParam(req, "store"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (r *Registry) checkout(w http.ResponseWriter, req *http.Request) {
	if !r.originOK(w, req) {
		return
	}
	storeID := chi.URLParam(req, "store")
	wave := chi.URLParam(req, "wave")
	st, ok := r.Get(storeID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "store expired"})
		return
	}
	customer := membershipFrom(req, r.cfg.HMACSecret)
	r.mu.Lock()
	defer r.mu.Unlock()
	ls := r.stores[storeID]
	if ls == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "store expired"})
		return
	}
	ls.seq++
	o := Order{
		ID:       "ord_" + strconv.Itoa(ls.seq),
		StoreID:  storeID,
		Customer: customer,
		Wave:     wave,
		At:       time.Now().UTC(),
	}
	ls.orders = append(ls.orders, o)
	writeJSON(w, http.StatusCreated, map[string]any{
		"order_id": o.ID,
		"store":    st.Name,
		"product":  st.Product,
		"price":    st.Price,
		"customer": customer,
	})
}

func (r *Registry) originOK(w http.ResponseWriter, req *http.Request) bool {
	if r.cfg.OriginSecret == "" {
		return true
	}
	got := req.Header.Get("X-Bruiser-Origin-Secret")
	if subtle.ConstantTimeCompare([]byte(got), []byte(r.cfg.OriginSecret)) != 1 {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error":  "origin lockdown",
			"detail": "sandbox checkout requires Bruiser admission",
		})
		return false
	}
	return true
}

func (r *Registry) maybeIssueFanCookie(w http.ResponseWriter, req *http.Request) {
	if r.cfg.HMACSecret == "" {
		return
	}
	if c, err := req.Cookie(CookieName); err == nil && c.Value != "" {
		return
	}
	tok, err := auth.IssueBoxOfficeSession(r.cfg.HMACSecret, "fan-"+id.New("id")[3:], time.Hour)
	if err != nil {
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func membershipFrom(req *http.Request, secret string) string {
	c, err := req.Cookie(CookieName)
	if err != nil {
		return ""
	}
	a, err := auth.ParseAssertionHS256(c.Value, secret, "bruiser")
	if err != nil {
		return ""
	}
	return a.CustomerID
}

func productHTML(st Store) string {
	name := html.EscapeString(st.Name)
	product := html.EscapeString(st.Product)
	price := html.EscapeString(st.Price)
	return `<!doctype html>
<html lang="en">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>` + name + ` — Limited drop</title>
<style>
  :root { --bg:#090b10; --ink:#f4efe4; --muted:#9a9386; --accent:#d6ff4a; --line:rgba(244,239,228,.12); }
  * { box-sizing: border-box; }
  body { margin:0; min-height:100vh; background:
    radial-gradient(1200px 500px at 50% -10%, rgba(214,255,74,.12), transparent 55%),
    var(--bg); color:var(--ink); font-family: ui-sans-serif, system-ui, sans-serif; }
  main { max-width: 520px; margin: 12vh auto; padding: 0 24px; text-align:center; }
  .kicker { letter-spacing:.28em; font-size:11px; color:var(--accent); font-weight:700; }
  h1 { font-family: Georgia, "Iowan Old Style", serif; font-weight:500; font-size: clamp(2rem, 6vw, 3.2rem); line-height:1.05; margin:18px 0 8px; }
  .sub { color:var(--muted); margin-bottom:28px; }
  .price { font-size:2rem; font-variant-numeric: tabular-nums; margin: 8px 0 28px; }
  .buy { appearance:none; border:0; background:var(--accent); color:#111; font-weight:800; letter-spacing:.04em;
         padding:16px 28px; border-radius:999px; font-size:15px; cursor:pointer; }
  .buy:hover { filter: brightness(1.05); }
  .note { margin-top:28px; color:var(--muted); font-size:12px; }
  .card { border:1px solid var(--line); border-radius:28px; padding:40px 32px 36px; background:rgba(255,255,255,.03); }
</style>
<main>
  <div class="card">
    <div class="kicker">LIMITED DROP</div>
    <h1>` + name + ` × Special Edition</h1>
    <p class="sub">` + product + `</p>
    <div class="price">` + price + `</div>
    <form method="post" action="/s/` + html.EscapeString(st.ID) + `/checkout">
      <button class="buy" type="submit">BUY NOW</button>
    </form>
    <p class="note">Sandbox storefront. Checkout only succeeds through Bruiser.</p>
  </div>
</main>
</html>`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func Sanitize(s string, fallback string, max int) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\x00", "")
	if s == "" {
		return fallback
	}
	if max > 0 && len(s) > max {
		s = s[:max]
	}
	return s
}
