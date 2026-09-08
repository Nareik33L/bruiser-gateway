package check_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	publicapi "github.com/Nareik33L/bruiser-gateway/internal/api/public"
	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
	"github.com/Nareik33L/bruiser-gateway/internal/simtix"
	pgstore "github.com/Nareik33L/bruiser-gateway/internal/store/postgres"
	"github.com/Nareik33L/bruiser-gateway/internal/testlab"
)

const advCustomer = "adv-alice"

// TestV1AdversarialOneCustomer is the final V1 attack pass: a malicious
// customer, a buggy agent, and a misconfigured client try to obtain two
// origin executions for one customer. max_active=1 must hold.
func TestV1AdversarialOneCustomer(t *testing.T) {
	lab := startAdversarialLab(t)
	t.Cleanup(func() { lab.assertActiveAtMost(t, 1) })

	t.Run("concurrent-acquire", func(t *testing.T) { advConcurrentAcquire(t, lab) })
	t.Run("reconnect-storm", func(t *testing.T) { advReconnectStorm(t, lab) })
	t.Run("renew-release-storm", func(t *testing.T) { advRenewReleaseStorm(t, lab) })
	t.Run("stale-lease-resurrection", func(t *testing.T) { advStaleLease(t, lab) })
	t.Run("old-token-after-handoff", func(t *testing.T) { advHandoffReplay(t, lab) })
	t.Run("revoked-token", func(t *testing.T) { advRevokedToken(t, lab) })
	t.Run("customer-id-switching", func(t *testing.T) { advCustomerSwitch(t, lab) })
	t.Run("merchant-id-switching", func(t *testing.T) { advMerchantSwitch(t, lab) })
	t.Run("alternate-http-routes", func(t *testing.T) { advAlternateRoutes(t, lab) })
	t.Run("direct-origin", func(t *testing.T) { advDirectOrigin(t, lab) })
	t.Run("malformed-credentials", func(t *testing.T) { advMalformed(t, lab) })
	t.Run("replayed-credentials", func(t *testing.T) { advReplay(t, lab) })
	t.Run("simultaneous-replicas", func(t *testing.T) { advReplicaSplit(t, lab) })
	t.Run("store-outage", func(t *testing.T) { advStoreOutage(t, lab) })
	t.Run("proxy-and-bruiser-restart", func(t *testing.T) { advRestart(t, lab) })
	t.Run("timeouts", func(t *testing.T) { advTimeouts(t, lab) })
}

type advLab struct {
	watch   *originWatch
	origin  string
	frontA  string
	frontB  string
	gwA     string
	gwB     string
	hmac    string
	admin   string
	cfg     testlab.Lab
	apiB    *publicapi.Server
	srvB    *httptest.Server
	storeB  *pgstore.Store
	sticky  string
	stolen  string // last forwarded execution JWT captured at origin
}

type originWatch struct {
	mu      sync.Mutex
	exes    map[string]int
	phase   map[string]int
	bare    int
	lastTok string
	next    http.Handler
}

func startAdversarialLab(t *testing.T) *advLab {
	t.Helper()
	profile := testlab.ArsenalProfile(t)
	core := testlab.Start(t, profile, nil)
	ctx := context.Background()
	core.API.Start(ctx)

	storeB, err := pgstore.Connect(ctx, core.Cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(storeB.Close)
	apiB := publicapi.New(core.Cfg, storeB, core.Signer, slog.New(slog.NewTextHandler(io.Discard, nil)), profile)
	t.Cleanup(apiB.Close)
	srvB := httptest.NewServer(apiB)
	t.Cleanup(srvB.Close)
	apiB.Start(ctx)

	watch := &originWatch{exes: map[string]int{}, phase: map[string]int{}}
	origin := simtix.New(simtix.Config{HMACSecret: core.Cfg.DevHMACSecret, OriginSecret: core.Cfg.OriginSecret, Seats: 500})
	watch.next = origin.Handler()
	originSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Millisecond)
		watch.ServeHTTP(w, r)
	}))
	t.Cleanup(originSrv.Close)

	phA, err := core.API.ProxyHandler(originSrv.URL)
	if err != nil {
		t.Fatal(err)
	}
	phB, err := apiB.ProxyHandler(originSrv.URL)
	if err != nil {
		t.Fatal(err)
	}
	frontA := httptest.NewServer(phA)
	t.Cleanup(frontA.Close)
	frontB := httptest.NewServer(phB)
	t.Cleanup(frontB.Close)

	sticky, err := auth.IssueBoxOfficeSession(core.Cfg.DevHMACSecret, advCustomer, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return &advLab{
		watch:  watch,
		origin: originSrv.URL,
		frontA: frontA.URL,
		frontB: frontB.URL,
		gwA:    core.Server.URL,
		gwB:    srvB.URL,
		hmac:   core.Cfg.DevHMACSecret,
		admin:  core.Cfg.AdminSecret,
		cfg:    core,
		apiB:   apiB,
		srvB:   srvB,
		storeB: storeB,
		sticky: sticky,
	}
}

func (w *originWatch) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	rec := httptest.NewRecorder()
	w.next.ServeHTTP(rec, r)
	if strings.Contains(r.URL.Path, "/holds") && rec.Code == http.StatusCreated {
		w.note(r.Header.Get("X-Bruiser-Execution"))
	}
	for k, vs := range rec.Header() {
		for _, v := range vs {
			rw.Header().Add(k, v)
		}
	}
	rw.WriteHeader(rec.Code)
	_, _ = rw.Write(rec.Body.Bytes())
}

func (w *originWatch) note(tok string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	id := exeIDFromJWT(tok)
	if id == "" {
		w.bare++
		return
	}
	w.exes[id]++
	w.lastTok = tok
	if w.phase == nil {
		w.phase = map[string]int{}
	}
	w.phase[id]++
}

func (w *originWatch) beginPhase() {
	w.mu.Lock()
	w.phase = map[string]int{}
	w.mu.Unlock()
}

func (w *originWatch) phaseUnique() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.phase)
}

func (w *originWatch) token() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastTok
}

func exeIDFromJWT(tok string) string {
	parts := strings.Split(tok, ".")
	if len(parts) < 2 {
		return ""
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return ""
		}
	}
	var m map[string]any
	if json.Unmarshal(payload, &m) != nil {
		return ""
	}
	if v, ok := m["exe"].(string); ok {
		return v
	}
	return ""
}

func (a *advLab) assertActiveAtMost(t *testing.T, n int) {
	t.Helper()
	got := a.active(t)
	if len(got) > n {
		t.Fatalf("ACTIVE executions for %s = %d want <= %d: %v", advCustomer, len(got), n, got)
	}
	if a.watch.bare > 0 {
		t.Fatalf("origin accepted %d holds without an execution token", a.watch.bare)
	}
}

func (a *advLab) assertPhaseAtMost(t *testing.T, n int) {
	t.Helper()
	a.assertActiveAtMost(t, 1)
	if u := a.watch.phaseUnique(); u > n {
		t.Fatalf("this phase created %d distinct origin executions want <= %d", u, n)
	}
}

func (a *advLab) active(t *testing.T) []string {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, a.gwB+"/v1/admin/status", nil)
	req.Header.Set("X-Bruiser-Admin-Secret", a.admin)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var st struct {
		Executions []struct {
			ID       string `json:"execution_id"`
			Customer string `json:"customer_id"`
		} `json:"executions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, e := range st.Executions {
		if e.Customer == advCustomer {
			ids = append(ids, e.ID)
		}
	}
	return ids
}

func (a *advLab) currentID(t *testing.T) string {
	t.Helper()
	ids := a.active(t)
	if len(ids) != 1 {
		t.Fatalf("want exactly one active, got %v", ids)
	}
	return ids[0]
}

func (a *advLab) hold(t *testing.T, front, cookie string) int {
	t.Helper()
	return a.holdClient(t, http.DefaultClient, front, cookie, nil)
}

func (a *advLab) holdClient(t *testing.T, c *http.Client, front, cookie string, extra map[string]string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, front+"/api/events/ars-che/holds", bytes.NewBufferString(`{"seats":1}`))
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: simtix.CookieName, Value: cookie})
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		return 0
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode
}

func (a *advLab) cookie(t *testing.T) string {
	t.Helper()
	tok, err := auth.IssueBoxOfficeSession(a.hmac, advCustomer, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func (a *advLab) session(t *testing.T, gw, ptype, principal string) string {
	t.Helper()
	assertion, err := auth.IssueDevAssertion(a.hmac, advCustomer, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"principal":{"type":%q,"id":%q}}`, ptype, principal)
	req, _ := http.NewRequest(http.MethodPost, gw+"/v1/sessions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+assertion)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("session %d %s", resp.StatusCode, raw)
	}
	var out struct {
		Token string `json:"session_token"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.Token == "" {
		t.Fatalf("session token %s", raw)
	}
	return out.Token
}

func (a *advLab) postAuth(t *testing.T, url, body, bearer string) (int, string, map[string]any) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = bytes.NewBufferString(body)
	}
	req, _ := http.NewRequest(http.MethodPost, url, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp.StatusCode, string(raw), m
}

func (a *advLab) putControls(t *testing.T, body string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, a.gwB+"/v1/admin/controls", bytes.NewBufferString(body))
	req.Header.Set("X-Bruiser-Admin-Secret", a.admin)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("controls %d %s", resp.StatusCode, b)
	}
}

func (a *advLab) originHeld(t *testing.T) int {
	t.Helper()
	resp, err := http.Get(a.origin + "/api/events/ars-che")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var ev struct {
		Held int `json:"held"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ev); err != nil {
		t.Fatal(err)
	}
	return ev.Held
}

func (a *advLab) captureStolen(t *testing.T) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, a.gwB+"/v1/authorize", bytes.NewBufferString(`{"method":"POST","path":"/api/events/ars-che/holds"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Bruiser-Edge-Secret", a.cfg.Cfg.EdgeSecret)
	req.Header.Set("Cookie", simtix.CookieName+"="+a.sticky)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	tok := resp.Header.Get("X-Bruiser-Execution")
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if tok == "" {
		tok = a.watch.token()
	}
	a.stolen = tok
}

func advConcurrentAcquire(t *testing.T, a *advLab) {
	a.watch.beginPhase()
	const n = 400
	var created, busy, other atomic.Int64
	var winner atomic.Value
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			front := a.frontA
			if i%2 == 1 {
				front = a.frontB
			}
			c := a.sticky
			if i != 0 {
				c = a.cookie(t)
			}
			code := a.hold(t, front, c)
			switch code {
			case http.StatusCreated:
				created.Add(1)
				winner.Store(c)
			case http.StatusConflict:
				busy.Add(1)
			default:
				other.Add(1)
			}
		}()
	}
	wg.Wait()
	if created.Load() != 1 || other.Load() != 0 {
		t.Fatalf("created=%d busy=%d other=%d want 1/%d/0", created.Load(), busy.Load(), other.Load(), n-1)
	}
	if w, ok := winner.Load().(string); ok && w != "" {
		a.sticky = w
	}
	if a.originHeld(t) != 1 {
		t.Fatalf("origin held=%d want 1 after 400-way swarm", a.originHeld(t))
	}
	a.assertPhaseAtMost(t, 1)
	a.captureStolen(t)
}

func advReconnectStorm(t *testing.T, a *advLab) {
	a.watch.beginPhase()
	const n = 200
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			front := a.frontA
			if i%2 == 1 {
				front = a.frontB
			}
			_ = a.hold(t, front, a.sticky)
		}()
	}
	wg.Wait()
	a.assertPhaseAtMost(t, 1)
	a.assertActiveAtMost(t, 1)
}

func advRenewReleaseStorm(t *testing.T, a *advLab) {
	a.watch.beginPhase()
	exe := a.currentID(t)
	browser := a.session(t, a.gwB, "browser", "tab")
	code, raw, body := a.postAuth(t, a.gwB+"/v1/executions/acquire", `{"resource":"event:ars-che","action":"hold"}`, browser)
	if code != http.StatusConflict {
		t.Fatalf("browser acquire want 409 got %d %s", code, raw)
	}
	if body["can_preempt"] != true {
		t.Fatalf("browser should be able to take control: %s", raw)
	}
	code, raw, hand := a.postAuth(t, a.gwB+"/v1/executions/"+exe+"/handoff", `{"mode":"preempt"}`, browser)
	if code != http.StatusCreated {
		t.Fatalf("handoff %d %s", code, raw)
	}
	succ, _ := hand["execution_id"].(string)

	var wg sync.WaitGroup
	const n = 80
	wg.Add(n + n + 1)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, _, _ = a.postAuth(t, a.gwB+"/v1/executions/"+succ+"/renew", "", browser)
		}()
		go func() {
			defer wg.Done()
			_ = a.hold(t, a.frontA, a.cookie(t))
		}()
	}
	go func() {
		defer wg.Done()
		_, _, _ = a.postAuth(t, a.gwA+"/v1/executions/"+succ+"/release", "", browser)
	}()
	wg.Wait()
	a.assertActiveAtMost(t, 1)
	if a.watch.phaseUnique() > 1 {
		t.Fatalf("renew/release storm created %d origin executions", a.watch.phaseUnique())
	}
}

func advStaleLease(t *testing.T, a *advLab) {
	a.putControls(t, `{"lease_ttl_seconds":1,"updated_by":"adv"}`)
	t.Cleanup(func() { a.putControls(t, `{"lease_ttl_seconds":0,"updated_by":"adv"}`) })
	if ids := a.active(t); len(ids) == 1 {
		tok := a.session(t, a.gwB, "browser", "exp-rev")
		_, _, _ = a.postAuth(t, a.gwB+"/v1/executions/"+ids[0]+"/revoke", `{"reason":"stale-setup"}`, tok)
	}
	seed := a.cookie(t)
	if code := a.hold(t, a.frontB, seed); code != http.StatusCreated {
		t.Fatalf("seed after revoke want 201 got %d", code)
	}
	old := a.currentID(t)
	time.Sleep(2200 * time.Millisecond)
	a.watch.beginPhase()
	const n = 80
	var created atomic.Int64
	var wg sync.WaitGroup
	wg.Add(n + 10)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			front := a.frontA
			if i%2 == 1 {
				front = a.frontB
			}
			if a.hold(t, front, a.cookie(t)) == http.StatusCreated {
				created.Add(1)
			}
		}()
	}
	dead := a.session(t, a.gwA, "agent", "zombie")
	for i := 0; i < 10; i++ {
		go func() {
			defer wg.Done()
			_, _, _ = a.postAuth(t, a.gwA+"/v1/executions/"+old+"/renew", "", dead)
		}()
	}
	wg.Wait()
	if created.Load() != 1 {
		t.Fatalf("stale-lease race granted %d want 1", created.Load())
	}
	a.assertPhaseAtMost(t, 1)
}

func advHandoffReplay(t *testing.T, a *advLab) {
	exe := a.currentID(t)
	agent := a.session(t, a.gwA, "agent", "shopping")
	code, raw, _ := a.postAuth(t, a.gwA+"/v1/executions/acquire", `{"resource":"event:ars-che","action":"hold"}`, agent)
	if code != http.StatusConflict {
		t.Fatalf("agent acquire want 409 got %d %s", code, raw)
	}
	browser := a.session(t, a.gwB, "browser", "take")
	code, raw, _ = a.postAuth(t, a.gwB+"/v1/executions/"+exe+"/handoff", `{"mode":"preempt"}`, browser)
	if code != http.StatusCreated && code != http.StatusConflict {
		// If the current holder is already a browser, preempt from another browser may fail.
		// Take control from the recorded holder via revoke+reacquire instead.
		if code != http.StatusCreated {
			t.Logf("handoff %d %s — falling back to replay of stolen token", code, raw)
		}
	}
	a.watch.beginPhase()
	if a.stolen != "" {
		req, _ := http.NewRequest(http.MethodPost, a.origin+"/api/events/ars-che/holds", bytes.NewBufferString(`{"seats":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Bruiser-Execution", a.stolen)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusCreated {
			t.Fatal("old execution token must not hold at a locked origin")
		}
	}
	if code, raw, _ = a.postAuth(t, a.gwB+"/v1/executions/"+exe+"/renew", "", agent); code == http.StatusOK {
		t.Fatalf("old holder renew after handoff/revoke must not succeed: %s", raw)
	}
	a.assertPhaseAtMost(t, 0)
}

func advRevokedToken(t *testing.T, a *advLab) {
	ids := a.active(t)
	if len(ids) == 0 {
		if a.hold(t, a.frontB, a.cookie(t)) != http.StatusCreated {
			t.Fatal("need an active execution to revoke")
		}
		ids = a.active(t)
	}
	browser := a.session(t, a.gwB, "browser", "stop")
	code, raw, _ := a.postAuth(t, a.gwB+"/v1/executions/"+ids[0]+"/revoke", `{"reason":"customer_stop"}`, browser)
	if code != http.StatusOK {
		t.Fatalf("revoke %d %s", code, raw)
	}
	a.watch.beginPhase()
	if code, raw, _ = a.postAuth(t, a.gwB+"/v1/executions/"+ids[0]+"/renew", "", browser); code == http.StatusOK {
		t.Fatalf("renew after revoke: %s", raw)
	}
	if a.stolen != "" {
		req, _ := http.NewRequest(http.MethodPost, a.origin+"/api/events/ars-che/holds", bytes.NewBufferString(`{"seats":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Bruiser-Execution", a.stolen)
		req.Header.Set("X-Bruiser-Origin-Secret", "spoofed")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusCreated {
			t.Fatal("revoked token + spoofed origin secret allocated")
		}
	}
	a.assertPhaseAtMost(t, 0)
	if a.hold(t, a.frontB, a.cookie(t)) != http.StatusCreated {
		t.Fatal("reacquire after revoke")
	}
	a.assertActiveAtMost(t, 1)
}

func advCustomerSwitch(t *testing.T, a *advLab) {
	a.watch.beginPhase()
	bob, err := auth.IssueBoxOfficeSession(a.hmac, "adv-bob", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if code := a.holdClient(t, http.DefaultClient, a.frontA, a.sticky, map[string]string{
		"X-Bruiser-Customer": "adv-bob",
		"X-Customer-Id":      "adv-bob",
		"Authorization":      "Bearer " + bob,
	}); code == http.StatusCreated && a.watch.phaseUnique() > 1 {
		t.Fatalf("identity switch created a second origin execution (%d)", code)
	}
	if code := a.holdClient(t, http.DefaultClient, a.frontB, bob, nil); code != http.StatusCreated {
		t.Fatalf("bob is a different customer and may hold: %d", code)
	}
	if a.hold(t, a.frontA, a.cookie(t)) == http.StatusCreated && len(a.active(t)) > 1 {
		t.Fatal("alice must still be a single active")
	}
	a.assertActiveAtMost(t, 1)
}

func advMerchantSwitch(t *testing.T, a *advLab) {
	a.watch.beginPhase()
	_, other, cfg, _ := testlab.GatewayAPI(t, testlab.ArsenalProfile(t))
	assertion, err := auth.IssueDevAssertion(cfg.DevHMACSecret, advCustomer, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	body := `{"principal":{"type":"agent","id":"x"}}`
	req, _ := http.NewRequest(http.MethodPost, other.URL+"/v1/sessions", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+assertion)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var sess struct {
		Token string `json:"session_token"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&sess)
	resp.Body.Close()
	if sess.Token == "" {
		t.Fatal("other merchant session")
	}
	code, raw, _ := a.postAuth(t, a.gwA+"/v1/executions/acquire", `{"resource":"event:ars-che","action":"hold"}`, sess.Token)
	if code == http.StatusCreated || code == http.StatusOK {
		t.Fatalf("foreign merchant session must not acquire here: %d %s", code, raw)
	}
	if a.holdClient(t, http.DefaultClient, a.frontA, "", map[string]string{"Authorization": "Bearer " + sess.Token}) == http.StatusCreated {
		t.Fatal("foreign session as Bearer must not hold")
	}
	a.assertPhaseAtMost(t, 0)
}

func advAlternateRoutes(t *testing.T, a *advLab) {
	a.watch.beginPhase()
	paths := append([]string{
		"/api/events/ars-che/hold",
		"/api/events/ars-che/holds/",
	}, merchant.CommonAllocationPaths...)
	for _, p := range paths {
		for _, front := range []string{a.frontA, a.origin} {
			req, _ := http.NewRequest(http.MethodPost, front+p, bytes.NewBufferString(`{"seats":1,"event_id":"ars-che"}`))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: simtix.CookieName, Value: a.sticky})
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusCreated && p != "/api/orders" {
				if a.watch.phaseUnique() > 0 && !strings.HasSuffix(p, "/holds") {
					t.Fatalf("unlisted %s on %s allocated", p, front)
				}
			}
		}
	}
	a.assertActiveAtMost(t, 1)
	if a.watch.bare > 0 {
		t.Fatal("alternate route produced a bare origin hold")
	}
}

func advDirectOrigin(t *testing.T, a *advLab) {
	a.watch.beginPhase()
	attempts := []map[string]string{
		nil,
		{"Cookie": simtix.CookieName + "=" + a.sticky},
		{"X-Bruiser-Customer": advCustomer},
		{"X-Bruiser-Execution": "stale.not.a.jwt"},
		{"X-Bruiser-Origin-Secret": "wrong"},
		{"X-Bruiser-Origin-Secret": a.cfg.Cfg.OriginSecret, "X-Bruiser-Execution": "stale.not.a.jwt"},
	}
	for _, hdr := range attempts {
		req, _ := http.NewRequest(http.MethodPost, a.origin+"/api/events/ars-che/holds", bytes.NewBufferString(`{"seats":1}`))
		req.Header.Set("Content-Type", "application/json")
		for k, v := range hdr {
			if k == "Cookie" {
				req.Header.Set("Cookie", v)
				continue
			}
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode == http.StatusCreated {
			t.Fatalf("direct origin allocated with %v", hdr)
		}
	}
	a.assertPhaseAtMost(t, 0)
}

func advMalformed(t *testing.T, a *advLab) {
	a.watch.beginPhase()
	bads := []string{"", "not-a-jwt", "aaa.bbb.ccc", a.sticky[:len(a.sticky)/2], a.sticky + "x"}
	for _, c := range bads {
		if code := a.hold(t, a.frontA, c); code == http.StatusCreated {
			t.Fatalf("malformed cookie allocated (%q)", c)
		}
	}
	req, _ := http.NewRequest(http.MethodPost, a.gwA+"/v1/sessions", bytes.NewBufferString(`{`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode < 400 {
		t.Fatalf("malformed session %d", resp.StatusCode)
	}
	a.assertPhaseAtMost(t, 0)
}

func advReplay(t *testing.T, a *advLab) {
	a.watch.beginPhase()
	a.captureStolen(t)
	if a.stolen == "" {
		t.Fatal("expected a live execution token from authorize")
	}
	for i := 0; i < 40; i++ {
		if code := a.holdClient(t, http.DefaultClient, a.frontB, a.sticky, map[string]string{
			"X-Bruiser-Execution": a.stolen,
		}); code == http.StatusCreated && a.watch.phaseUnique() > 1 {
			t.Fatal("replayed execution minted a second origin execution")
		}
	}
	req, _ := http.NewRequest(http.MethodPost, a.origin+"/api/events/ars-che/holds", bytes.NewBufferString(`{"seats":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Bruiser-Execution", a.stolen)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusCreated {
		t.Fatal("replayed token at origin without secret allocated")
	}
	a.assertPhaseAtMost(t, 1)
}

func advReplicaSplit(t *testing.T, a *advLab) {
	a.watch.beginPhase()
	const n = 200
	var created atomic.Int64
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			front := a.frontA
			if i%2 == 1 {
				front = a.frontB
			}
			if a.hold(t, front, a.cookie(t)) == http.StatusCreated {
				created.Add(1)
			}
		}()
	}
	wg.Wait()
	if created.Load() > 1 {
		t.Fatalf("replica split granted %d", created.Load())
	}
	a.assertPhaseAtMost(t, 1)
}

func advStoreOutage(t *testing.T, a *advLab) {
	a.watch.beginPhase()
	a.cfg.Store.Close()
	var forwarded atomic.Int64
	var wg sync.WaitGroup
	wg.Add(40)
	for i := 0; i < 40; i++ {
		go func() {
			defer wg.Done()
			if a.hold(t, a.frontA, a.cookie(t)) == http.StatusCreated {
				forwarded.Add(1)
			}
		}()
	}
	wg.Wait()
	if forwarded.Load() != 0 {
		t.Fatalf("store outage fail-open granted %d origin holds", forwarded.Load())
	}
	a.assertPhaseAtMost(t, 0)
	if a.hold(t, a.frontB, a.cookie(t)) == http.StatusCreated && len(a.active(t)) > 1 {
		t.Fatal("healthy replica must not grant a second active")
	}
	a.assertActiveAtMost(t, 1)
}

func advRestart(t *testing.T, a *advLab) {
	a.watch.beginPhase()
	ph, err := a.apiB.ProxyHandler(a.origin)
	if err != nil {
		t.Fatal(err)
	}
	front := httptest.NewServer(ph)
	t.Cleanup(front.Close)
	a.cfg.Server.Close()

	storeC, err := pgstore.Connect(context.Background(), a.cfg.Cfg.DatabaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(storeC.Close)
	apiC := publicapi.New(a.cfg.Cfg, storeC, a.cfg.Signer, slog.New(slog.NewTextHandler(io.Discard, nil)), testlab.ArsenalProfile(t))
	t.Cleanup(apiC.Close)
	phC, err := apiC.ProxyHandler(a.origin)
	if err != nil {
		t.Fatal(err)
	}
	frontC := httptest.NewServer(phC)
	t.Cleanup(frontC.Close)

	var created atomic.Int64
	var wg sync.WaitGroup
	wg.Add(80)
	for i := 0; i < 80; i++ {
		i := i
		go func() {
			defer wg.Done()
			f := front.URL
			if i%2 == 1 {
				f = frontC.URL
			}
			if a.hold(t, f, a.cookie(t)) == http.StatusCreated {
				created.Add(1)
			}
		}()
	}
	wg.Wait()
	if created.Load() > 1 {
		t.Fatalf("restart granted %d", created.Load())
	}
	a.assertPhaseAtMost(t, 1)
}

func advTimeouts(t *testing.T, a *advLab) {
	a.watch.beginPhase()
	fast := &http.Client{Timeout: 2 * time.Millisecond}
	var wg sync.WaitGroup
	wg.Add(60)
	for i := 0; i < 60; i++ {
		go func() {
			defer wg.Done()
			_ = a.holdClient(t, fast, a.frontB, a.cookie(t), nil)
		}()
	}
	wg.Wait()
	if a.hold(t, a.frontB, a.cookie(t)) == http.StatusCreated && len(a.active(t)) > 1 {
		t.Fatal("timeout storm leaked a second active")
	}
	a.assertActiveAtMost(t, 1)
	a.assertPhaseAtMost(t, 1)
}
