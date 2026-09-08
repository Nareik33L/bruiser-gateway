// Package edge is the lab stand-in for a club WAF/CDN calling POST /v1/authorize
// before forwarding to the box-office origin. It is a deployment method, not a product.
package edge

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

type Config struct {
	OriginURL     string
	BruiserURL    string
	EdgeSecret    string
	MaxInFlight   int
	AuthorizePath string
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
	ou, err := url.Parse(cfg.OriginURL)
	if err != nil {
		return nil, err
	}
	return &Proxy{
		cfg:      cfg,
		origin:   ou,
		bruiser:  strings.TrimRight(cfg.BruiserURL, "/"),
		client:   &http.Client{Timeout: 10 * time.Second},
		inflight: map[string]int{},
	}, nil
}

func (p *Proxy) Handler() http.Handler {
	rp := httputil.NewSingleHostReverseProxy(p.origin)
	origDirector := rp.Director
	rp.Director = func(req *http.Request) {
		origDirector(req)
		req.Host = p.origin.Host
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))

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

		if authResp.StatusCode != http.StatusOK {
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
			w.WriteHeader(authResp.StatusCode)
			_, _ = w.Write(authBody)
			return
		}

		exe := authResp.Header.Get("X-Bruiser-Execution")
		if exe != "" {
			if !p.take(exe) {
				w.Header().Set("Retry-After", "1")
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
		}
		rp.ServeHTTP(w, r)
	})
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
