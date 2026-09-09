package attacklab

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Nareik33L/bruiser-gateway/internal/demostore"
	"github.com/Nareik33L/bruiser-gateway/internal/edge"
)

func (l *Lab) API() http.Handler {
	r := chi.NewRouter()
	r.Post("/stores", l.createStore)
	r.Get("/stores/{id}", l.getStore)
	r.Post("/stores/{id}/mode", l.setMode)
	r.Post("/stores/{id}/attacks", l.startAttack)
	r.Get("/stores/{id}/attacks/{aid}", l.getAttack)
	r.Get("/stores/{id}/attacks/{aid}/events", l.streamAttack)
	return r
}

type createBody struct {
	Name    string `json:"name"`
	Product string `json:"product"`
	Price   string `json:"price"`
}

func (l *Lab) createStore(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	var extra map[string]any
	if err := json.Unmarshal(raw, &extra); err != nil && len(raw) > 0 {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	for k, v := range extra {
		if forbiddenKey(k) {
			writeErr(w, http.StatusBadRequest, "sandbox only; target URLs are not accepted")
			return
		}
		if s, ok := v.(string); ok && looksLikeURL(s) {
			writeErr(w, http.StatusBadRequest, "sandbox only; target URLs are not accepted")
			return
		}
	}
	var body createBody
	_ = json.Unmarshal(raw, &body)
	name := demostore.Sanitize(body.Name, "Bruiser FC", 80)
	product := demostore.Sanitize(body.Product, "Limited Edition Shirt", 80)
	price := demostore.Sanitize(body.Price, "£50.00", 24)
	if looksLikeURL(name) || looksLikeURL(product) || looksLikeURL(price) {
		writeErr(w, http.StatusBadRequest, "sandbox only; target URLs are not accepted")
		return
	}

	ip := clientIP(r)
	l.mu.Lock()
	if l.origin.Count() >= MaxStores {
		l.mu.Unlock()
		writeErr(w, http.StatusTooManyRequests, "too many sandbox stores")
		return
	}
	now := time.Now()
	window := now.Add(-30 * time.Minute)
	kept := l.creates[ip][:0]
	for _, t := range l.creates[ip] {
		if t.After(window) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= StoresPerIP {
		l.creates[ip] = kept
		l.mu.Unlock()
		writeErr(w, http.StatusTooManyRequests, "rate limited")
		return
	}
	l.creates[ip] = append(kept, now)
	l.mu.Unlock()

	st := l.origin.Create(name, product, price, l.cfg.StoreTTL)
	l.mu.Lock()
	l.sessions[st.ID] = &session{Store: st, Mode: edge.ModeDryRun, IP: ip}
	l.mu.Unlock()

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         st.ID,
		"name":       st.Name,
		"product":    st.Product,
		"price":      st.Price,
		"mode":       edge.ModeDryRun,
		"url":        "/s/" + st.ID,
		"expires_at": st.ExpiresAt.Format(time.RFC3339),
		"status":     "live",
	})
}

func (l *Lab) getStore(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s, ok := l.getSession(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "store expired")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":         s.Store.ID,
		"name":       s.Store.Name,
		"product":    s.Store.Product,
		"price":      s.Store.Price,
		"mode":       s.Mode,
		"url":        "/s/" + s.Store.ID,
		"expires_at": s.Store.ExpiresAt.Format(time.RFC3339),
		"status":     "live",
	})
}

func (l *Lab) setMode(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	s, ok := l.getSession(id)
	if !ok {
		writeErr(w, http.StatusNotFound, "store expired")
		return
	}
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	switch body.Mode {
	case edge.ModeOff, edge.ModeDryRun, edge.ModeEnforce:
	default:
		writeErr(w, http.StatusBadRequest, "mode must be off, dry-run, or enforce")
		return
	}
	l.mu.Lock()
	s.Mode = body.Mode
	l.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"id": id, "mode": s.Mode})
}

func (l *Lab) startAttack(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "id")
	s, ok := l.getSession(storeID)
	if !ok {
		writeErr(w, http.StatusNotFound, "store expired")
		return
	}
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	var extra map[string]any
	_ = json.Unmarshal(raw, &extra)
	for k := range extra {
		if forbiddenKey(k) {
			writeErr(w, http.StatusBadRequest, "sandbox only; target URLs are not accepted")
			return
		}
	}
	var body struct {
		SwarmSize int  `json:"swarm_size"`
		Replay    bool `json:"replay"`
	}
	_ = json.Unmarshal(raw, &body)
	size := body.SwarmSize
	if size == 0 {
		size = DefaultSwarm
	}
	if size < MinSwarm || size > MaxSwarm {
		writeErr(w, http.StatusBadRequest, fmt.Sprintf("swarm_size must be %d–%d", MinSwarm, MaxSwarm))
		return
	}

	l.mu.Lock()
	if s.attacking {
		l.mu.Unlock()
		writeErr(w, http.StatusConflict, "attack already running")
		return
	}
	if l.running >= MaxConcurrentAttacks {
		l.mu.Unlock()
		writeErr(w, http.StatusTooManyRequests, "simulator busy")
		return
	}
	if !body.Replay && !s.lastAt.IsZero() && time.Since(s.lastAt) < attackCooldown {
		l.mu.Unlock()
		writeErr(w, http.StatusTooManyRequests, "slow down")
		return
	}
	if l.cfg.Gateway == nil && l.cfg.BruiserURL == "" && s.Mode != edge.ModeOff {
		l.mu.Unlock()
		writeErr(w, http.StatusServiceUnavailable, "gateway unavailable")
		return
	}

	agents := s.Agents
	if !body.Replay || len(agents) == 0 || s.LastSize != size {
		agents = compose(size, storeID)
	}
	s.Agents = agents
	s.LastSize = size
	s.attacking = true
	l.running++
	att := newAttack(storeID, s.Mode, size)
	l.attacks[att.ID] = att
	l.mu.Unlock()

	go l.runAttack(att, agents)

	writeJSON(w, http.StatusAccepted, map[string]any{
		"attack_id":  att.ID,
		"store_id":   storeID,
		"mode":       att.Mode,
		"swarm_size": size,
		"wave":       att.Wave,
		"stream":     "/lab/stores/" + storeID + "/attacks/" + att.ID + "/events",
	})
}

func (l *Lab) getAttack(w http.ResponseWriter, r *http.Request) {
	att, ok := l.lookupAttack(chi.URLParam(r, "id"), chi.URLParam(r, "aid"))
	if !ok {
		writeErr(w, http.StatusNotFound, "attack not found")
		return
	}
	m, done, sum := att.snapshot()
	writeJSON(w, http.StatusOK, map[string]any{
		"id":         att.ID,
		"store_id":   att.StoreID,
		"mode":       att.Mode,
		"swarm_size": att.SwarmSize,
		"done":       done,
		"metrics":    m,
		"summary":    sum,
	})
}

func (l *Lab) streamAttack(w http.ResponseWriter, r *http.Request) {
	att, ok := l.lookupAttack(chi.URLParam(r, "id"), chi.URLParam(r, "aid"))
	if !ok {
		writeErr(w, http.StatusNotFound, "attack not found")
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
	ch, cancel, replay := att.subscribe()
	defer cancel()
	writeSSE := func(e Event) {
		b, _ := json.Marshal(e)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, b)
		flusher.Flush()
	}
	for _, e := range replay {
		writeSSE(e)
	}
	_, done, _ := att.snapshot()
	if done {
		return
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			writeSSE(e)
			if e.Type == "done" || e.Type == "error" {
				return
			}
		}
	}
}

func (l *Lab) lookupAttack(storeID, aid string) (*Attack, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.attacks[aid]
	if !ok || a.StoreID != storeID {
		return nil, false
	}
	return a, true
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
