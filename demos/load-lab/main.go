package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Nareik33L/bruiser-gateway/demos/seed"
	"github.com/Nareik33L/bruiser-gateway/demos/shared"
)

const pathClubEdge = "club → Edge hold/order"

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	addr := shared.Env("LOADLAB_HTTP_ADDR", ":8120")
	lab := newLab(log)
	srv := &http.Server{Addr: addr, Handler: lab.routes(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 120 * time.Second}
	log.Info("load-lab listening", "addr", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error("serve", "err", err)
		os.Exit(1)
	}
}

type lab struct {
	log         *slog.Logger
	adminSecret string
	clubURL     string
	edgeURL     string
	maxAgents   int

	mu      sync.Mutex
	run     *run
	running bool
}

type run struct {
	cancel context.CancelFunc
	events []event
	subs   []chan event
	sum    Summary
	done   bool
	preset string
}

type event struct {
	Type       string `json:"type"`
	At         int64  `json:"at"`
	ID         int    `json:"id,omitempty"`
	Membership string `json:"membership,omitempty"`
	Path       string `json:"path,omitempty"`
	Status     string `json:"status,omitempty"`
	Detail     string `json:"detail,omitempty"`
}

type Summary struct {
	Agents        int    `json:"agents"`
	Authenticated int    `json:"authenticated"`
	Attempts      int    `json:"attempts"`
	Allowed       int    `json:"allowed"`
	Busy          int    `json:"busy"`
	Denied        int    `json:"denied"`
	Orders        int    `json:"orders"`
	Waiting       int    `json:"waiting"`
	Finished      int    `json:"finished"`
	Error         string `json:"error,omitempty"`
	ElapsedMS     int64  `json:"elapsed_ms"`
}

func newLab(log *slog.Logger) *lab {
	maxA, _ := strconv.Atoi(shared.Env("LOADLAB_MAX_AGENTS", "10000"))
	if maxA < 1 {
		maxA = 10000
	}
	return &lab{
		log:         log,
		adminSecret: shared.Env("DEMO_ADMIN_SECRET", "demo-admin-dev"),
		clubURL:     shared.Env("HARCHESTER_URL", "http://127.0.0.1:8100"),
		edgeURL:     shared.Env("SIMTIX_EDGE_URL", "http://127.0.0.1:8091"),
		maxAgents:   maxA,
	}
}

func (l *lab) routes() http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		shared.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "load-lab"})
	})
	r.Get("/status", l.auth(l.status))
	r.Post("/run", l.auth(l.start))
	r.Post("/stop", l.auth(l.stop))
	r.Get("/events", l.auth(l.stream))
	return r
}

func (l *lab) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Demo-Admin-Secret") != l.adminSecret {
			shared.WriteErr(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r)
	}
}

type startReq struct {
	Membership      string `json:"membership_number"`
	Password        string `json:"password"`
	Agents          int    `json:"agents"`
	SpawnPerS       int    `json:"spawn_per_s"`
	RetrySec        int    `json:"retry_window_sec"`
	Preset          string `json:"preset"` // single | ten | multi
	Supporters      int    `json:"supporters"`
	StartMembership int    `json:"start_membership"`
}

func (l *lab) start(w http.ResponseWriter, r *http.Request) {
	var req startReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		shared.WriteErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Password == "" {
		req.Password = seed.DefaultPassword
	}
	if req.Agents < 1 {
		req.Agents = 1
	}
	if req.Agents > l.maxAgents {
		req.Agents = l.maxAgents
	}
	if req.SpawnPerS < 1 {
		req.SpawnPerS = 500
	}
	if req.RetrySec < 1 {
		req.RetrySec = 8
	}
	switch req.Preset {
	case "multi", "ten":
	default:
		req.Preset = "single"
	}
	if req.Preset == "single" && req.Membership == "" {
		req.Membership = seed.AliceMembership
	}
	if req.Supporters < 1 {
		req.Supporters = 1000
	}
	if req.StartMembership < 1 {
		req.StartMembership = seed.FirstMembership
	}

	l.mu.Lock()
	if l.running {
		l.mu.Unlock()
		shared.WriteErr(w, http.StatusConflict, "run in progress")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	rn := &run{cancel: cancel, preset: req.Preset}
	l.run = rn
	l.running = true
	l.mu.Unlock()

	go l.execute(ctx, rn, req)
	body := map[string]any{"status": "started", "agents": req.Agents, "preset": req.Preset}
	if req.Preset == "ten" {
		body["memberships"] = seed.TenMemberships()
	}
	shared.WriteJSON(w, http.StatusAccepted, body)
}

func (l *lab) stop(w http.ResponseWriter, _ *http.Request) {
	l.mu.Lock()
	if l.run != nil {
		l.run.cancel()
	}
	l.mu.Unlock()
	shared.WriteJSON(w, http.StatusOK, map[string]string{"status": "stopping"})
}

func (l *lab) status(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	body := map[string]any{"running": l.running}
	if l.run != nil {
		events := l.run.events
		if len(events) > 80 {
			events = events[len(events)-80:]
		}
		body["summary"] = l.run.sum
		body["done"] = l.run.done
		body["preset"] = l.run.preset
		body["events"] = append([]event(nil), events...)
	}
	shared.WriteJSON(w, http.StatusOK, body)
}

func (l *lab) stream(w http.ResponseWriter, r *http.Request) {
	shared.SSEHeaders(w)
	l.mu.Lock()
	if l.run == nil {
		l.mu.Unlock()
		_ = shared.SSEEvent(w, "idle", map[string]string{"status": "idle"})
		return
	}
	ch := make(chan event, 64)
	l.run.subs = append(l.run.subs, ch)
	replay := append([]event(nil), l.run.events...)
	l.mu.Unlock()
	for _, e := range replay {
		_ = shared.SSEEvent(w, e.Type, e)
	}
	flusher, _ := w.(http.Flusher)
	for {
		select {
		case <-r.Context().Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			_ = shared.SSEEvent(w, e.Type, e)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

func (l *lab) execute(ctx context.Context, rn *run, req startReq) {
	defer func() {
		l.mu.Lock()
		l.running = false
		rn.done = true
		for _, ch := range rn.subs {
			close(ch)
		}
		l.mu.Unlock()
	}()
	start := time.Now()
	var authN, attempts, allowed, busy, denied, orders, waiting, finished atomic.Int64
	sem := make(chan struct{}, 500)
	var wg sync.WaitGroup
	interval := time.Second / time.Duration(req.SpawnPerS)
	if interval < time.Millisecond {
		interval = time.Millisecond
	}
	tenIDs := seed.TenMemberships()
	multiIDs := sequentialEligible(req.StartMembership, req.Supporters)

	snapshot := func() Summary {
		return Summary{
			Agents: req.Agents, Authenticated: int(authN.Load()), Attempts: int(attempts.Load()),
			Allowed: int(allowed.Load()), Busy: int(busy.Load()), Denied: int(denied.Load()),
			Orders: int(orders.Load()), Waiting: int(waiting.Load()), Finished: int(finished.Load()),
			ElapsedMS: time.Since(start).Milliseconds(),
		}
	}
	publish := func(e event) {
		e.At = time.Now().UnixMilli()
		rn.events = append(rn.events, e)
		if len(rn.events) > 400 {
			rn.events = rn.events[len(rn.events)-300:]
		}
		for _, ch := range rn.subs {
			select {
			case ch <- e:
			default:
			}
		}
	}

	for i := 0; i < req.Agents; i++ {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		idx := i
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			mem := membershipFor(req, idx, tenIDs, multiIDs)
			res := l.agent(ctx, mem, req.Password, time.Duration(req.RetrySec)*time.Second)
			if res.authenticated {
				authN.Add(1)
			}
			attempts.Add(int64(res.attempts))
			allowed.Add(int64(res.allowed))
			busy.Add(int64(res.busy))
			denied.Add(int64(res.denied))
			if res.ordered {
				orders.Add(1)
			}
			if res.waiting {
				waiting.Add(1)
			}
			finished.Add(1)
			publishLive := idx < 80 || res.ordered || idx%25 == 0 || idx == req.Agents-1
			l.mu.Lock()
			rn.sum = snapshot()
			if publishLive {
				publish(event{
					Type: "agent", ID: idx + 1, Membership: mem, Path: pathClubEdge,
					Status: res.status, Detail: res.detail,
				})
			}
			l.mu.Unlock()
		}()
		select {
		case <-ctx.Done():
		case <-time.After(interval):
		}
	}
	wg.Wait()
	l.mu.Lock()
	rn.sum = snapshot()
	publish(event{Type: "done", Detail: "complete", Path: pathClubEdge})
	l.mu.Unlock()
	l.log.Info("run complete", "summary", rn.sum)
}

func sequentialEligible(start, n int) []string {
	if n < 1 {
		n = 1
	}
	out := make([]string, 0, n)
	for num := start; len(out) < n && num < start+n*4+50; num++ {
		if seed.EligibleMembershipNumber(num) {
			out = append(out, strconv.Itoa(num))
		}
	}
	return out
}

func membershipFor(req startReq, idx int, ten, multi []string) string {
	switch req.Preset {
	case "ten":
		if len(ten) == 0 {
			return seed.AliceMembership
		}
		return ten[idx%len(ten)]
	case "multi":
		if len(multi) == 0 {
			return strconv.Itoa(req.StartMembership + (idx % req.Supporters))
		}
		return multi[idx%len(multi)]
	default:
		return req.Membership
	}
}

type agentRes struct {
	authenticated, ordered, waiting bool
	attempts, allowed, busy, denied int
	status, detail                  string
}

func (l *lab) agent(ctx context.Context, membership, password string, retryWindow time.Duration) agentRes {
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	var out agentRes
	loginBody, _ := json.Marshal(map[string]string{"membership_number": membership, "password": password})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, l.clubURL+"/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		out.status = "denied"
		out.detail = "login-error: " + err.Error()
		out.denied++
		return out
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusFound {
		out.status = "denied"
		out.detail = "login-fail"
		out.denied++
		return out
	}
	out.authenticated = true

	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, l.clubURL+"/tickets/hfc-ars/buy", nil)
	resp, err = client.Do(req)
	if err != nil {
		out.status = "denied"
		out.detail = "buy-error"
		out.denied++
		return out
	}
	loc := resp.Header.Get("Location")
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()
	if loc == "" {
		out.status = "denied"
		out.detail = "no-handoff"
		out.denied++
		return out
	}
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, loc, nil)
	resp, err = client.Do(req)
	if err != nil {
		out.status = "denied"
		out.detail = "sso-error"
		out.denied++
		return out
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	_ = resp.Body.Close()

	deadline := time.Now().Add(retryWindow)
	holdURL := l.edgeURL + "/api/events/" + seed.HeadlineEventID + "/holds"
	for {
		out.attempts++
		req, _ = http.NewRequestWithContext(ctx, http.MethodPost, holdURL, bytes.NewBufferString(`{"seats":1}`))
		req.Header.Set("Content-Type", "application/json")
		resp, err = client.Do(req)
		if err != nil {
			out.denied++
			out.status = "denied"
			out.detail = "hold-error"
			return out
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		_ = resp.Body.Close()
		decision := resp.Header.Get("X-Bruiser-Lab-Decision")
		kind := classifyHold(decision, resp.StatusCode)
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			kind = "busy"
		}
		switch kind {
		case "allow":
			out.allowed++
			var h struct {
				ID string `json:"hold_id"`
			}
			_ = json.Unmarshal(b, &h)
			if h.ID == "" {
				var alt struct {
					ID string `json:"id"`
				}
				_ = json.Unmarshal(b, &alt)
				h.ID = alt.ID
			}
			if h.ID == "" {
				out.status = "allow"
				out.detail = "hold-no-id"
				return out
			}
			if l.completeOrder(ctx, client, h.ID, deadline) {
				out.ordered = true
				out.status = "order"
				return out
			}
			out.status = "allow"
			out.detail = "hold-no-order"
			return out
		case "busy":
			out.busy++
			out.waiting = true
			if time.Now().After(deadline) || ctx.Err() != nil {
				out.status = "busy"
				return out
			}
			select {
			case <-ctx.Done():
				out.status = "busy"
				return out
			case <-time.After(2 * time.Second):
			}
			continue
		default:
			out.denied++
			out.status = "denied"
			out.detail = originDetail(b)
			return out
		}
	}
}

func (l *lab) completeOrder(ctx context.Context, client *http.Client, holdID string, deadline time.Time) bool {
	payload := `{"event_id":"` + seed.HeadlineEventID + `","hold_id":"` + holdID + `"}`
	for i := 0; i < 8; i++ {
		if ctx.Err() != nil {
			return false
		}
		oreq, _ := http.NewRequestWithContext(ctx, http.MethodPost, l.edgeURL+"/api/orders", bytes.NewBufferString(payload))
		oreq.Header.Set("Content-Type", "application/json")
		oresp, err := client.Do(oreq)
		if err != nil {
			if time.Now().After(deadline) {
				return false
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(oresp.Body, 1<<16))
		_ = oresp.Body.Close()
		kind := classifyHold(oresp.Header.Get("X-Bruiser-Lab-Decision"), oresp.StatusCode)
		if oresp.StatusCode == http.StatusTooManyRequests || oresp.StatusCode >= 500 {
			kind = "busy"
		}
		if oresp.StatusCode >= 200 && oresp.StatusCode < 300 {
			return true
		}
		if kind == "busy" || oresp.StatusCode == http.StatusForbidden || oresp.StatusCode >= 500 {
			if time.Now().After(deadline) {
				return false
			}
			select {
			case <-ctx.Done():
				return false
			case <-time.After(500 * time.Millisecond):
			}
			continue
		}
		_ = b
		return false
	}
	return false
}

// classifyHold maps Edge + origin outcomes. Bruiser BUSY is only the
// Edge decision header. Origin 409 (per-account limit / sold out) is
// denied, including when Off stamps X-Bruiser-Lab-Decision: ALLOW.
func classifyHold(decision string, status int) string {
	switch strings.ToUpper(strings.TrimSpace(decision)) {
	case "BUSY":
		return "busy"
	case "DENIED":
		if status >= 200 && status < 300 {
			return "allow"
		}
		return "denied"
	}
	if status >= 200 && status < 300 {
		return "allow"
	}
	return "denied"
}

func originDetail(b []byte) string {
	var m map[string]string
	if json.Unmarshal(b, &m) == nil {
		if m["error"] != "" {
			return m["error"]
		}
	}
	s := strings.TrimSpace(string(b))
	if len(s) > 120 {
		s = s[:120]
	}
	return s
}
