// Package edge is the lab stand-in for a club WAF/CDN calling POST /v1/authorize
// before forwarding to the box-office origin. It is a deployment method, not a product.
package edge

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	ModeEnforce = "enforce"
	ModeDryRun  = "dry-run"
	ModeOff     = "off"

	HeaderDecision   = "X-Bruiser-Lab-Decision"
	HeaderForwarded  = "X-Bruiser-Lab-Forwarded"
	HeaderWouldBlock = "X-Bruiser-Would-Block"
)

type Config struct {
	OriginURL      string
	BruiserURL     string
	EdgeSecret     string
	OriginSecret   string
	MaxInFlight    int
	AuthorizePath  string
	Mode           string
	ModeFn         func(*http.Request) string
	OriginHandler  http.Handler
	BruiserHandler http.Handler
}

type Proxy struct {
	cfg      Config
	origin   *url.URL
	bruiser  string
	client   *http.Client
	mu       sync.Mutex
	inflight map[string]int
}

func New(cfg Config) (*Proxy, error) {
	if cfg.AuthorizePath == "" {
		cfg.AuthorizePath = "/v1/authorize"
	}
	if cfg.MaxInFlight <= 0 {
		cfg.MaxInFlight = 2
	}
	if cfg.Mode == "" {
		cfg.Mode = ModeEnforce
	}
	if cfg.OriginURL == "" {
		if cfg.OriginHandler == nil {
			return nil, fmt.Errorf("origin url or handler required")
		}
		cfg.OriginURL = "http://origin.internal"
	}
	ou, err := url.Parse(cfg.OriginURL)
	if err != nil {
		return nil, err
	}
	bruiser := strings.TrimRight(cfg.BruiserURL, "/")
	if bruiser == "" {
		bruiser = "http://bruiser.internal"
	}
	client := &http.Client{Timeout: 10 * time.Second}
	if cfg.BruiserHandler != nil {
		client.Transport = handlerTransport{h: cfg.BruiserHandler}
	}
	return &Proxy{
		cfg:      cfg,
		origin:   ou,
		bruiser:  bruiser,
		client:   client,
		inflight: map[string]int{},
	}, nil
}

func (p *Proxy) mode(r *http.Request) string {
	if p.cfg.ModeFn != nil {
		if m := p.cfg.ModeFn(r); m != "" {
			return m
		}
	}
	if p.cfg.Mode == "" {
		return ModeEnforce
	}
	return p.cfg.Mode
}

func (p *Proxy) Handler() http.Handler {
	rp := httputil.NewSingleHostReverseProxy(p.origin)
	if p.cfg.OriginHandler != nil {
		rp.Transport = handlerTransport{h: p.cfg.OriginHandler}
	}
	origDirector := rp.Director
	rp.Director = func(req *http.Request) {
		origDirector(req)
		req.Host = p.origin.Host
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))

		mode := p.mode(r)
		if mode == ModeOff {
			p.forward(w, r, rp, body, "", "", true, false)
			return
		}

		authReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, p.bruiser+p.cfg.AuthorizePath, bytes.NewReader(authorizeBody(r, body)))
		if err != nil {
			http.Error(w, `{"error":"authorize"}`, http.StatusBadGateway)
			return
		}
		authReq.Header.Set("Content-Type", "application/json")
		authReq.Header.Set("X-Original-Method", r.Method)
		authReq.Header.Set("X-Original-URI", r.URL.RequestURI())
		authReq.Header.Set("X-Bruiser-Edge-Secret", p.cfg.EdgeSecret)
		if c := r.Header.Get("Cookie"); c != "" {
			authReq.Header.Set("Cookie", c)
		}
		if a := r.Header.Get("Authorization"); a != "" {
			authReq.Header.Set("Authorization", a)
		}
		if eid := eventID(body); eid != "" {
			authReq.Header.Set("X-Bruiser-Event-Id", eid)
		}

		authResp, err := p.client.Do(authReq)
		if err != nil {
			http.Error(w, `{"error":"authorize unavailable"}`, http.StatusBadGateway)
			return
		}
		authBody, _ := io.ReadAll(io.LimitReader(authResp.Body, 1<<20))
		_ = authResp.Body.Close()

		decision := decisionFrom(authResp.StatusCode, authBody)
		wouldBlock := decision == "BUSY" || decision == "DENIED" || (authResp.StatusCode != http.StatusOK && decision != "ALLOW")

		if authResp.StatusCode != http.StatusOK && mode != ModeDryRun {
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
			w.Header().Set(HeaderDecision, decision)
			w.Header().Set(HeaderForwarded, "0")
			if wouldBlock {
				w.Header().Set(HeaderWouldBlock, "1")
			}
			w.WriteHeader(authResp.StatusCode)
			_, _ = w.Write(authBody)
			return
		}

		exe := authResp.Header.Get("X-Bruiser-Execution")
		if exe != "" && mode == ModeEnforce {
			if !p.take(exe) {
				w.Header().Set("Retry-After", "1")
				w.Header().Set(HeaderDecision, "THROTTLED")
				w.Header().Set(HeaderForwarded, "0")
				http.Error(w, `{"error":"execution throttled"}`, http.StatusTooManyRequests)
				return
			}
			defer p.release(exe)
		}

		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		r.Header.Set("X-Bruiser-Execution", exe)
		r.Header.Set("X-Bruiser-Fence", authResp.Header.Get("X-Bruiser-Fence"))
		r.Header.Set("X-Bruiser-Customer", authResp.Header.Get("X-Bruiser-Customer"))
		if sec := authResp.Header.Get("X-Bruiser-Origin-Secret"); sec != "" {
			r.Header.Set("X-Bruiser-Origin-Secret", sec)
		} else if mode == ModeDryRun && p.cfg.OriginSecret != "" {
			r.Header.Set("X-Bruiser-Origin-Secret", p.cfg.OriginSecret)
		}
		p.forward(w, r, rp, body, decision, exe, true, wouldBlock)
	})
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
		w.Header().Set(HeaderDecision, w.decision)
		if w.forwarded {
			w.Header().Set(HeaderForwarded, "1")
		} else {
			w.Header().Set(HeaderForwarded, "0")
		}
		if w.wouldBlock {
			w.Header().Set(HeaderWouldBlock, "1")
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

func (p *Proxy) forward(w http.ResponseWriter, r *http.Request, rp *httputil.ReverseProxy, body []byte, decision string, _ string, forwarded, wouldBlock bool) {
	if decision == "" {
		decision = "ALLOW"
	}
	if p.cfg.OriginSecret != "" && r.Header.Get("X-Bruiser-Origin-Secret") == "" {
		r.Header.Set("X-Bruiser-Origin-Secret", p.cfg.OriginSecret)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	out := &decisionWriter{
		ResponseWriter: w,
		decision:       decision,
		forwarded:      forwarded,
		wouldBlock:     wouldBlock,
	}
	rp.ServeHTTP(out, r)
}

func (p *Proxy) take(exe string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.inflight[exe] >= p.cfg.MaxInFlight {
		return false
	}
	p.inflight[exe]++
	return true
}

func (p *Proxy) release(exe string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.inflight[exe]--
	if p.inflight[exe] <= 0 {
		delete(p.inflight, exe)
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

func decisionFrom(status int, body []byte) string {
	var m map[string]any
	if json.Unmarshal(body, &m) == nil {
		if v, ok := m["status"].(string); ok && v != "" {
			return strings.ToUpper(v)
		}
		if v, ok := m["error"].(string); ok && status >= 400 {
			if strings.Contains(strings.ToLower(v), "unauthor") {
				return "DENIED"
			}
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
	case status == http.StatusUnauthorized, status == http.StatusForbidden:
		return "DENIED"
	default:
		return "DENIED"
	}
}

func eventID(body []byte) string {
	var m map[string]any
	if json.Unmarshal(body, &m) != nil {
		return ""
	}
	if v, ok := m["event_id"].(string); ok {
		return v
	}
	return ""
}

func authorizeBody(r *http.Request, raw []byte) []byte {
	payload := map[string]string{
		"method": r.Method,
		"path":   r.URL.Path,
	}
	if eid := eventID(raw); eid != "" {
		payload["event_id"] = eid
	}
	b, _ := json.Marshal(payload)
	return b
}
