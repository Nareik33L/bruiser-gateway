package main

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Nareik33L/bruiser-gateway/demos/shared"
)

const (
	modeOff     = "off"
	modeDryRun  = "dry-run"
	modeEnforce = "enforce"

	headerDecision   = "X-Bruiser-Lab-Decision"
	headerForwarded  = "X-Bruiser-Lab-Forwarded"
	headerWouldBlock = "X-Bruiser-Would-Block"
)

type edgeConfig struct {
	OriginHandler http.Handler
	BruiserURL    string
	EdgeSecret    string
	OriginSecret  string
	AdminSecret   string
	HMACSecret    string
}

type edge struct {
	cfg     edgeConfig
	client  *http.Client
	proxy   *httputil.ReverseProxy
	mu      sync.Mutex
	mode    string
	percent int
	stats   edgeStats
}

type edgeStats struct {
	AuthorizeCalls int `json:"authorize_calls"`
	Forwarded      int `json:"forwarded"`
	Busy           int `json:"busy"`
	Denied         int `json:"denied"`
	WouldBlock     int `json:"would_block"`
	Throttled      int `json:"throttled"`
}

func newEdge(cfg edgeConfig) *edge {
	originURL, _ := url.Parse("http://origin.internal")
	rp := httputil.NewSingleHostReverseProxy(originURL)
	rp.Transport = handlerTransport{h: cfg.OriginHandler}
	orig := rp.Director
	rp.Director = func(req *http.Request) {
		orig(req)
		req.Host = "origin.internal"
	}
	return &edge{
		cfg: cfg,
		client: &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        512,
				MaxIdleConnsPerHost: 512,
			},
		},
		proxy:   rp,
		mode:    modeEnforce,
		percent: 100,
	}
}

func (e *edge) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_edge/config" {
			e.handleConfig(w, r)
			return
		}
		if r.URL.Path == "/_edge/stats" {
			e.handleStats(w, r)
			return
		}
		if r.URL.Path == "/_edge/reset-stats" && r.Method == http.MethodPost {
			if !e.adminOK(w, r) {
				return
			}
			e.resetStats()
			shared.WriteJSON(w, http.StatusOK, map[string]string{"status": "reset"})
			return
		}
		e.serve(w, r)
	})
}

func (e *edge) adminOK(w http.ResponseWriter, r *http.Request) bool {
	got := r.Header.Get("X-Demo-Admin-Secret")
	if subtle.ConstantTimeCompare([]byte(got), []byte(e.cfg.AdminSecret)) != 1 {
		shared.WriteErr(w, http.StatusUnauthorized, "bad admin secret")
		return false
	}
	return true
}

func (e *edge) handleConfig(w http.ResponseWriter, r *http.Request) {
	if !e.adminOK(w, r) {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if r.Method == http.MethodPut || r.Method == http.MethodPost {
		var body struct {
			Mode    string `json:"mode"`
			Percent *int   `json:"percent"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch body.Mode {
		case modeOff, modeDryRun, modeEnforce:
			e.mode = body.Mode
		}
		if body.Percent != nil {
			p := *body.Percent
			if p < 0 {
				p = 0
			}
			if p > 100 {
				p = 100
			}
			e.percent = p
			if p == 0 && e.mode == modeEnforce {
				e.mode = modeDryRun
			}
		}
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"mode": e.mode, "percent": e.percent})
}

func (e *edge) handleStats(w http.ResponseWriter, r *http.Request) {
	if !e.adminOK(w, r) {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	shared.WriteJSON(w, http.StatusOK, e.stats)
}

func (e *edge) snapshot() (mode string, percent int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.mode, e.percent
}

func (e *edge) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(body))

	if !isAllocation(r.Method, r.URL.Path) {
		e.forward(w, r, body, "ALLOW", true, false)
		return
	}

	mode, percent := e.snapshot()
	if mode == modeOff {
		e.injectOrigin(r)
		e.forward(w, r, body, "ALLOW", true, false)
		return
	}

	authReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, strings.TrimRight(e.cfg.BruiserURL, "/")+"/v1/authorize", bytes.NewReader(authorizeBody(r, body)))
	if err != nil {
		http.Error(w, `{"error":"authorize"}`, http.StatusBadGateway)
		return
	}
	authReq.Header.Set("Content-Type", "application/json")
	authReq.Header.Set("X-Original-Method", r.Method)
	authReq.Header.Set("X-Original-URI", r.URL.RequestURI())
	authReq.Header.Set("X-Bruiser-Edge-Secret", e.cfg.EdgeSecret)
	if c := r.Header.Get("Cookie"); c != "" {
		authReq.Header.Set("Cookie", c)
	}
	if a := r.Header.Get("Authorization"); a != "" {
		authReq.Header.Set("Authorization", a)
	}
	if eid := eventIDFromBody(body); eid != "" {
		authReq.Header.Set("X-Bruiser-Event-Id", eid)
	}

	e.mu.Lock()
	e.stats.AuthorizeCalls++
	e.mu.Unlock()

	authResp, err := e.client.Do(authReq)
	if err != nil {
		http.Error(w, `{"error":"authorize unavailable"}`, http.StatusBadGateway)
		return
	}
	authBody, _ := io.ReadAll(io.LimitReader(authResp.Body, 1<<20))
	_ = authResp.Body.Close()

	decision := decisionFrom(authResp.StatusCode, authBody)
	wouldBlock := decision == "BUSY" || decision == "DENIED" || (authResp.StatusCode != http.StatusOK && decision != "ALLOW")

	customer := membershipFrom(r, e.cfg.HMACSecret)
	inBucket := shared.Enforced(customer, percent)
	block := mode == modeEnforce && inBucket && wouldBlock

	if wouldBlock {
		e.mu.Lock()
		if decision == "BUSY" {
			e.stats.Busy++
		} else {
			e.stats.Denied++
		}
		e.stats.WouldBlock++
		e.mu.Unlock()
	}

	if block {
		for k, vs := range authResp.Header {
			if strings.EqualFold(k, "Content-Type") || strings.EqualFold(k, "Retry-After") {
				for _, v := range vs {
					w.Header().Add(k, v)
				}
			}
		}
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json")
		}
		w.Header().Set(headerDecision, decision)
		w.Header().Set(headerForwarded, "0")
		w.Header().Set(headerWouldBlock, "1")
		w.WriteHeader(authResp.StatusCode)
		_, _ = w.Write(authBody)
		return
	}

	r.Header.Set("X-Bruiser-Execution", authResp.Header.Get("X-Bruiser-Execution"))
	r.Header.Set("X-Bruiser-Fence", authResp.Header.Get("X-Bruiser-Fence"))
	r.Header.Set("X-Bruiser-Customer", authResp.Header.Get("X-Bruiser-Customer"))
	if sec := authResp.Header.Get("X-Bruiser-Origin-Secret"); sec != "" {
		r.Header.Set("X-Bruiser-Origin-Secret", sec)
	} else {
		e.injectOrigin(r)
	}
	e.forward(w, r, body, decision, true, wouldBlock)
}

func (e *edge) injectOrigin(r *http.Request) {
	if e.cfg.OriginSecret != "" && r.Header.Get("X-Bruiser-Origin-Secret") == "" {
		r.Header.Set("X-Bruiser-Origin-Secret", e.cfg.OriginSecret)
	}
}

func (e *edge) forward(w http.ResponseWriter, r *http.Request, body []byte, decision string, forwarded, wouldBlock bool) {
	if decision == "" {
		decision = "ALLOW"
	}
	e.injectOrigin(r)
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	if forwarded {
		e.mu.Lock()
		e.stats.Forwarded++
		e.mu.Unlock()
	}
	out := &decisionWriter{ResponseWriter: w, decision: decision, forwarded: forwarded, wouldBlock: wouldBlock}
	e.proxy.ServeHTTP(out, r)
}

type decisionWriter struct {
	http.ResponseWriter
	decision   string
	forwarded  bool
	wouldBlock bool
	wrote      bool
}

func (w *decisionWriter) WriteHeader(status int) {
	if !w.wrote {
		w.Header().Set(headerDecision, w.decision)
		if w.forwarded {
			w.Header().Set(headerForwarded, "1")
		} else {
			w.Header().Set(headerForwarded, "0")
		}
		if w.wouldBlock {
			w.Header().Set(headerWouldBlock, "1")
		}
		w.wrote = true
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *decisionWriter) Write(b []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(b)
}

func (w *decisionWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

type handlerTransport struct{ h http.Handler }

func (t handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body == nil {
		req.Body = http.NoBody
	}
	rec := httptest.NewRecorder()
	t.h.ServeHTTP(rec, req)
	return rec.Result(), nil
}

func isAllocation(method, path string) bool {
	if method != http.MethodPost {
		return false
	}
	if strings.HasSuffix(path, "/holds") && strings.Contains(path, "/api/events/") {
		return true
	}
	return path == "/api/orders"
}

func decisionFrom(status int, body []byte) string {
	var m map[string]any
	if json.Unmarshal(body, &m) == nil {
		if v, ok := m["status"].(string); ok && v != "" {
			return strings.ToUpper(v)
		}
		if v, ok := m["error"].(string); ok && status >= 400 {
			if v != "" {
				return "DENIED"
			}
		}
	}
	switch {
	case status == http.StatusOK:
		return "ALLOW"
	case status == http.StatusConflict:
		return "BUSY"
	default:
		return "DENIED"
	}
}

func authorizeBody(r *http.Request, raw []byte) []byte {
	payload := map[string]string{"method": r.Method, "path": r.URL.Path}
	if eid := eventIDFromBody(raw); eid != "" {
		payload["event_id"] = eid
	}
	b, _ := json.Marshal(payload)
	return b
}

func eventIDFromBody(raw []byte) string {
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	if v, ok := m["event_id"].(string); ok {
		return v
	}
	return ""
}

func (e *edge) resetStats() {
	e.mu.Lock()
	e.stats = edgeStats{}
	e.mu.Unlock()
}
