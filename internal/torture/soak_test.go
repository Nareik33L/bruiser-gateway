package torture

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	publicapi "github.com/Nareik33L/bruiser-gateway/internal/api/public"
	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/config"
	"github.com/Nareik33L/bruiser-gateway/internal/id"
	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
	"github.com/Nareik33L/bruiser-gateway/internal/policy"
	pgstore "github.com/Nareik33L/bruiser-gateway/internal/store/postgres"
	"gopkg.in/yaml.v3"
)

// TestSoakChurnSmoke is the CI-safe harness proof: short churn, one customer,
// renew/reconnect/release/queue, one replica restart, ACTIVE never exceeds 1.
func TestSoakChurnSmoke(t *testing.T) {
	runSoak(t, 20*time.Second, 200, 2*time.Second)
}

// TestSoakChurn is the long run: 10,000 agents behaving badly for
// BRUISER_SOAK_DURATION (make soak → 30m). Skipped unless that env is set.
func TestSoakChurn(t *testing.T) {
	raw := os.Getenv("BRUISER_SOAK_DURATION")
	if raw == "" {
		t.Skip("BRUISER_SOAK_DURATION not set (make soak)")
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < time.Second {
		t.Fatalf("BRUISER_SOAK_DURATION=%q", raw)
	}
	agents := 10000
	if v := os.Getenv("BRUISER_SOAK_AGENTS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 2 {
			t.Fatalf("BRUISER_SOAK_AGENTS=%q", v)
		}
		agents = n
	}
	runSoak(t, d, agents, 30*time.Second)
}

type soakSnap struct {
	t            time.Time
	allocMB      uint64
	goroutines   int
	dbAcquired   int32
	active       int
	queue        int
	reqs         int64
	errs         int64
	p50ms, p99ms float64
}

func runSoak(t *testing.T, dur time.Duration, agents int, sampleEvery time.Duration) {
	t.Helper()
	url := os.Getenv("BRUISER_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("BRUISER_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	if err := pgstore.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	store, err := pgstore.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)

	cfg := config.Load()
	cfg.DatabaseURL = url
	cfg.MerchantID = id.New("m")
	cfg.LeaseTTL = 15 * time.Second
	cfg.AdminSecret = "admin-secret-dev"
	cfg.EdgeSecret = "edge-secret-dev"
	cfg.DevAssertions = true
	cfg.RateSessions, cfg.RateAcquire, cfg.RateRenew, cfg.RateRelease = 1e6, 1e6, 1e6, 1e6
	cfg.RateAuthorize, cfg.RateMerchant, cfg.RateCustomer, cfg.RatePrincipal = 1e6, 1e6, 1e6, 1e6
	if err := store.EnsureMerchant(ctx, cfg.MerchantID, "soak", cfg.DevHMACSecret); err != nil {
		t.Fatal(err)
	}
	key, err := store.EnsureSigningKey(ctx, cfg.MerchantID)
	if err != nil {
		t.Fatal(err)
	}
	signer := auth.Signer{KID: key.KID, MerchantID: cfg.MerchantID, Private: key.Private, Public: key.Public}
	profile := merchant.Empty(cfg.MerchantID)

	type node struct {
		api *publicapi.Server
		st  *pgstore.Store
		srv *httptest.Server
	}
	var mu sync.Mutex
	nodes := make([]*node, 0, 4)
	addNode := func() *node {
		st, err := pgstore.Connect(ctx, url)
		if err != nil {
			t.Fatal(err)
		}
		api := publicapi.New(cfg, st, signer, slog.New(slog.NewTextHandler(io.Discard, nil)), profile)
		api.Start(ctx)
		srv := httptest.NewServer(api)
		n := &node{api: api, st: st, srv: srv}
		mu.Lock()
		nodes = append(nodes, n)
		mu.Unlock()
		return n
	}
	for i := 0; i < 3; i++ {
		addNode()
	}
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for _, n := range nodes {
			if n == nil {
				continue
			}
			n.api.Close()
			n.srv.Close()
			n.st.Close()
		}
	})

	doc := policy.DefaultDocument(cfg.MerchantID)
	doc.Domains[0].Waiting = policy.Waiting{Mode: "bounded", MaxWaiters: 8}
	doc.Domains[0].LeaseTTL = "15s"
	raw, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	putPolicy(t, nodes[0].srv.URL, cfg.AdminSecret, raw)

	tokens := make([]string, agents)
	principals := make([]string, agents)
	sem := make(chan struct{}, 64)
	var wg sync.WaitGroup
	wg.Add(agents)
	for i := 0; i < agents; i++ {
		i := i
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			principals[i] = fmt.Sprintf("agent-%d", i)
			tokens[i] = mustSession(t, nodes[i%3].srv, cfg, "alice", principals[i])
		}()
	}
	wg.Wait()

	sweepCtx, sweepStop := context.WithCancel(ctx)
	t.Cleanup(sweepStop)
	go func() {
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-sweepCtx.Done():
				return
			case <-tick.C:
				_, _ = store.ExpireDue(context.Background(), 200)
			}
		}
	}()

	var (
		reqs, errs, created, busy, queued, renewOK, released atomic.Int64
		tokMu                                                sync.Mutex
		latMu                                                sync.Mutex
		latencies                                            []time.Duration
		holderTok, holderID                                  atomic.Value
		maxActive                                            atomic.Int64
	)
	holderTok.Store("")
	holderID.Store("")

	pickNode := func() *httptest.Server {
		mu.Lock()
		defer mu.Unlock()
		live := make([]*httptest.Server, 0, len(nodes))
		for _, n := range nodes {
			if n != nil {
				live = append(live, n.srv)
			}
		}
		if len(live) == 0 {
			return nil
		}
		return live[time.Now().UnixNano()%int64(len(live))]
	}

	do := func(method, path, body, bearer string) (int, map[string]any, time.Duration) {
		start := time.Now()
		srv := pickNode()
		if srv == nil {
			return 0, nil, 0
		}
		var rdr io.Reader
		if body != "" {
			rdr = bytes.NewBufferString(body)
		}
		req, _ := http.NewRequest(method, srv.URL+path, rdr)
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		resp, err := http.DefaultClient.Do(req)
		elapsed := time.Since(start)
		if err != nil {
			errs.Add(1)
			return 0, nil, elapsed
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		reqs.Add(1)
		latMu.Lock()
		if len(latencies) < 4096 {
			latencies = append(latencies, elapsed)
		} else {
			latencies[int(reqs.Load())%4096] = elapsed
		}
		latMu.Unlock()
		return resp.StatusCode, m, elapsed
	}

	deadline := time.Now().Add(dur)
	workers := 64
	if agents < workers {
		workers = agents
	}
	workCtx, workStop := context.WithCancel(ctx)
	t.Cleanup(workStop)
	var work sync.WaitGroup
	work.Add(workers)
	for w := 0; w < workers; w++ {
		go func(wid int) {
			defer work.Done()
			i := wid
			for time.Now().Before(deadline) {
				if workCtx.Err() != nil {
					return
				}
				i = (i + workers) % agents
				if i < 0 {
					i = wid % agents
				}
				tokMu.Lock()
				tok := tokens[i]
				tokMu.Unlock()
				kind := i % 20
				switch {
				case kind < 12:
					code, body, _ := do(http.MethodPost, "/v1/executions/acquire", `{"resource":"event:final","action":"purchase"}`, tok)
					switch code {
					case http.StatusCreated:
						created.Add(1)
						if id, _ := body["execution_id"].(string); id != "" {
							holderID.Store(id)
							holderTok.Store(tok)
						}
					case http.StatusOK:
						if id, _ := body["execution_id"].(string); id != "" {
							holderID.Store(id)
							holderTok.Store(tok)
						}
					case http.StatusAccepted:
						queued.Add(1)
					case http.StatusConflict:
						busy.Add(1)
					case 0:
					default:
						if code >= 500 {
							errs.Add(1)
						}
					}
				case kind < 16:
					hid, _ := holderID.Load().(string)
					htok, _ := holderTok.Load().(string)
					if hid != "" && htok != "" {
						code, _, _ := do(http.MethodPost, "/v1/executions/"+hid+"/renew", "", htok)
						if code == http.StatusOK {
							renewOK.Add(1)
						}
					}
				case kind < 18:
					if srv := pickNode(); srv != nil {
						next := mustSession(t, srv, cfg, "alice", principals[i])
						tokMu.Lock()
						tokens[i] = next
						tokMu.Unlock()
					}
				default:
					hid, _ := holderID.Load().(string)
					htok, _ := holderTok.Load().(string)
					if hid != "" && htok != "" {
						code, _, _ := do(http.MethodPost, "/v1/executions/"+hid+"/release", "", htok)
						if code == http.StatusOK {
							released.Add(1)
							holderID.Store("")
							holderTok.Store("")
						}
					}
				}
			}
		}(w)
	}

	activeCount := func() (active, queue int) {
		_ = store.Pool().QueryRow(ctx, `
			select count(*) from executions
			where merchant_id=$1 and state='ACTIVE' and expires_at>now()`,
			cfg.MerchantID).Scan(&active)
		_ = store.Pool().QueryRow(ctx, `
			select count(*) from waiters
			where merchant_id=$1`, cfg.MerchantID).Scan(&queue)
		return active, queue
	}

	sample := func() soakSnap {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		st := store.Pool().Stat()
		a, q := activeCount()
		if int64(a) > maxActive.Load() {
			maxActive.Store(int64(a))
		}
		latMu.Lock()
		p50, p99 := pct(latencies, 0.50), pct(latencies, 0.99)
		latMu.Unlock()
		return soakSnap{
			t: time.Now(), allocMB: ms.Alloc / (1024 * 1024), goroutines: runtime.NumGoroutine(),
			dbAcquired: st.AcquiredConns(), active: a, queue: q,
			reqs: reqs.Load(), errs: errs.Load(), p50ms: p50, p99ms: p99,
		}
	}

	startSnap := sample()
	t.Logf("soak start agents=%d dur=%s alloc=%dMB goroutines=%d", agents, dur, startSnap.allocMB, startSnap.goroutines)

	killed := false
	added := false
	ticker := time.NewTicker(sampleEvery)
	defer ticker.Stop()
	for time.Now().Before(deadline) {
		select {
		case <-ticker.C:
			s := sample()
			if s.active > 1 {
				workStop()
				t.Fatalf("ACTIVE=%d mid-soak", s.active)
			}
			t.Logf("t+%s alloc=%dMB goroutines=%d db=%d active=%d queue=%d reqs=%d errs=%d p50=%.1fms p99=%.1fms created=%d busy=%d queued=%d renew=%d release=%d",
				time.Since(deadline.Add(-dur)).Truncate(time.Second), s.allocMB, s.goroutines, s.dbAcquired,
				s.active, s.queue, s.reqs, s.errs, s.p50ms, s.p99ms,
				created.Load(), busy.Load(), queued.Load(), renewOK.Load(), released.Load())
			elapsed := time.Since(deadline.Add(-dur))
			if !killed && elapsed >= dur/5*2 {
				mu.Lock()
				if len(nodes) > 0 && nodes[0] != nil {
					nodes[0].api.Close()
					nodes[0].srv.Close()
					nodes[0].st.Close()
					nodes[0] = nil
					killed = true
					t.Log("killed replica 0")
				}
				mu.Unlock()
			}
			if !added && elapsed >= dur/5*3 {
				addNode()
				added = true
				t.Log("added replacement replica")
			}
		case <-time.After(time.Until(deadline) + time.Second):
		}
	}
	workStop()
	work.Wait()

	end := sample()
	t.Logf("soak end alloc=%dMB goroutines=%d db=%d active=%d queue=%d reqs=%d errs=%d p50=%.1fms p99=%.1fms maxActive=%d",
		end.allocMB, end.goroutines, end.dbAcquired, end.active, end.queue, end.reqs, end.errs, end.p50ms, end.p99ms, maxActive.Load())

	if maxActive.Load() > 1 || end.active > 1 {
		t.Fatalf("ACTIVE leaked: max=%d end=%d", maxActive.Load(), end.active)
	}
	if end.reqs < 10 {
		t.Fatalf("too few requests: %d", end.reqs)
	}
	if end.errs > end.reqs/5 {
		t.Fatalf("error rate %d/%d", end.errs, end.reqs)
	}
	if end.goroutines > startSnap.goroutines*4+200 {
		t.Fatalf("goroutine leak: start=%d end=%d", startSnap.goroutines, end.goroutines)
	}
	if end.dbAcquired > 16 {
		t.Fatalf("db connections %d over pool max", end.dbAcquired)
	}
}

func putPolicy(t *testing.T, gw, admin string, raw []byte) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, gw+"/v1/policy", bytes.NewReader(raw))
	req.Header.Set("X-Bruiser-Admin-Secret", admin)
	req.Header.Set("Content-Type", "application/yaml")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put policy %d", resp.StatusCode)
	}
}

func pct(ds []time.Duration, p float64) float64 {
	if len(ds) == 0 {
		return 0
	}
	ms := make([]float64, len(ds))
	for i, d := range ds {
		ms[i] = float64(d.Microseconds()) / 1000
	}
	sort.Float64s(ms)
	idx := int(math.Round(p * float64(len(ms)-1)))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(ms) {
		idx = len(ms) - 1
	}
	return ms[idx]
}
