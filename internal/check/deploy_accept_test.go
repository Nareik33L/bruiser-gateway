package check_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/check"
	"github.com/Nareik33L/bruiser-gateway/internal/edge"
	"github.com/Nareik33L/bruiser-gateway/internal/ops"
	"github.com/Nareik33L/bruiser-gateway/internal/policy"
	"github.com/Nareik33L/bruiser-gateway/internal/simtix"
	"github.com/Nareik33L/bruiser-gateway/internal/testlab"
	bruiser "github.com/Nareik33L/bruiser-gateway/sdk/go"
	"gopkg.in/yaml.v3"
)

// TestV1DeploymentAcceptance is the V1 install proof: the same suite against
// Embedded, Edge, and Proxy. Shared admission is already covered. This checks
// that the integration boundaries (cookie copy, origin stamp, execution JWT,
// Authority Check) do not change outcomes.
//
// Embedded Dry Run is the one honest difference: 0% observe does not mint an
// execution token, so a RequireExecution origin stays locked. Edge/Proxy 0%
// stamps the origin secret and the unaware hold continues.
func TestV1DeploymentAcceptance(t *testing.T) {
	t.Parallel()
	for _, kind := range []deployKind{kindEmbedded, kindEdge, kindProxy} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()
			proveDeployment(t, startDeploy(t, kind))
		})
	}
}

type deployKind string

const (
	kindEmbedded deployKind = "embedded"
	kindEdge     deployKind = "edge"
	kindProxy    deployKind = "proxy"
)

type deployLab struct {
	kind      deployKind
	front     string
	origin    string
	gw        string
	hmac      string
	admin     string
	edge      string
	originSec string
	merchant  string
	signer    auth.Signer
}

type exeResult struct {
	Status      int
	Raw         string
	Header      http.Header
	ID          string
	Token       string
	Fence       int64
	Would       string
	Enforced    string
	CanPreempt  bool
	Reason      string
	SuccessorID string
}

func startDeploy(t *testing.T, kind deployKind) deployLab {
	t.Helper()
	api, gw, cfg, signer := testlab.GatewayAPI(t, testlab.ArsenalProfile(t))
	lab := deployLab{
		kind:      kind,
		gw:        gw.URL,
		hmac:      cfg.DevHMACSecret,
		admin:     cfg.AdminSecret,
		edge:      cfg.EdgeSecret,
		originSec: cfg.OriginSecret,
		merchant:  cfg.MerchantID,
		signer:    signer,
	}
	switch kind {
	case kindEmbedded:
		fences := &bruiser.FenceCache{}
		origin := simtix.New(simtix.Config{
			HMACSecret:       cfg.DevHMACSecret,
			RequireExecution: true,
			VerifyExecution: func(tok string) error {
				claims, err := bruiser.Verify(tok, signer.Public)
				if err != nil {
					return err
				}
				if err := fences.Accept(claims.Domain, claims.Fence); err != nil {
					return simtix.ErrStaleFence
				}
				return nil
			},
			Seats: 200,
		})
		originSrv := httptest.NewServer(origin.Handler())
		t.Cleanup(originSrv.Close)
		lab.origin = originSrv.URL
	case kindEdge:
		origin := simtix.New(simtix.Config{HMACSecret: cfg.DevHMACSecret, OriginSecret: cfg.OriginSecret, Seats: 200})
		originSrv := httptest.NewServer(origin.Handler())
		t.Cleanup(originSrv.Close)
		p, err := edge.New(edge.Config{OriginURL: originSrv.URL, BruiserURL: gw.URL, EdgeSecret: cfg.EdgeSecret, MaxInFlight: 8})
		if err != nil {
			t.Fatal(err)
		}
		front := httptest.NewServer(p.Handler())
		t.Cleanup(front.Close)
		lab.front = front.URL
		lab.origin = originSrv.URL
	case kindProxy:
		origin := simtix.New(simtix.Config{HMACSecret: cfg.DevHMACSecret, OriginSecret: cfg.OriginSecret, Seats: 200})
		originSrv := httptest.NewServer(origin.Handler())
		t.Cleanup(originSrv.Close)
		ph, err := api.ProxyHandler(originSrv.URL)
		if err != nil {
			t.Fatal(err)
		}
		front := httptest.NewServer(ph)
		t.Cleanup(front.Close)
		lab.front = front.URL
		lab.origin = originSrv.URL
	default:
		t.Fatalf("unknown placement %s", kind)
	}
	return lab
}

func proveDeployment(t *testing.T, d deployLab) {
	t.Helper()
	d.putControls(t, `{"enforce_percent":100,"enforcement":true,"mode":"enforce","updated_by":"deploy"}`)

	t.Run("authentication", func(t *testing.T) { proveAuthentication(t, d) })
	t.Run("customer-identity", func(t *testing.T) { proveCustomerIdentity(t, d) })
	t.Run("acquire", func(t *testing.T) { proveAcquire(t, d) })
	t.Run("renew", func(t *testing.T) { proveRenew(t, d) })
	t.Run("release", func(t *testing.T) { proveRelease(t, d) })
	t.Run("queue", func(t *testing.T) { proveQueue(t, d) })
	t.Run("handoff", func(t *testing.T) { proveHandoff(t, d) })
	t.Run("revoke", func(t *testing.T) { proveRevoke(t, d) })
	t.Run("expiry", func(t *testing.T) { proveExpiry(t, d) })
	t.Run("origin-lockdown", func(t *testing.T) { proveOriginLockdown(t, d) })
	t.Run("dry-run", func(t *testing.T) { proveDryRun(t, d) })
	t.Run("ramp-10", func(t *testing.T) { proveRamp10(t, d) })
	t.Run("enforcement-100", func(t *testing.T) { proveEnforcement100(t, d) })
	t.Run("authority-check", func(t *testing.T) { proveAuthority(t, d) })
}

func proveAuthentication(t *testing.T, d deployLab) {
	t.Helper()
	cust := d.cust("auth")
	if d.fronted() {
		if code, body, _ := d.holdFrontRaw(t, "", nil); code != http.StatusUnauthorized {
			t.Fatalf("missing session want 401 got %d %s", code, body)
		}
		if code, body, _ := d.holdFrontRaw(t, "", map[string]string{
			"X-Bruiser-Customer": cust,
			"X-Customer-Id":      cust,
			"Authorization":      "Bearer " + d.boxCookie(t, cust),
		}); code != http.StatusUnauthorized {
			t.Fatalf("cookie-jwt must ignore spoofed headers and Bearer: %d %s", code, body)
		}
		expired, err := auth.IssueBoxOfficeSession(d.hmac, cust, -time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		if code, body, _ := d.holdFrontRaw(t, expired, nil); code != http.StatusUnauthorized {
			t.Fatalf("expired cookie want 401 got %d %s", code, body)
		}
		code, body, _ := d.holdFront(t, cust)
		if code != http.StatusCreated {
			t.Fatalf("authenticated hold want 201 got %d %s", code, body)
		}
		return
	}
	req, _ := http.NewRequest(http.MethodPost, d.gw+"/v1/sessions", bytes.NewBufferString(`{"principal":{"type":"agent","id":"a"}}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("session without assertion want 401 got %d", resp.StatusCode)
	}
	tok := d.session(t, cust, "agent", "auth-1")
	got := d.acquire(t, tok)
	if got.Status != http.StatusCreated || got.Token == "" {
		t.Fatalf("authenticated acquire want 201+token got %d %s", got.Status, got.Raw)
	}
	if code := d.holdOrigin(t, got.Token); code != http.StatusCreated {
		t.Fatalf("embedded origin hold %d", code)
	}
}

func proveCustomerIdentity(t *testing.T, d deployLab) {
	t.Helper()
	alice, bob := d.cust("ida"), d.cust("idb")
	if code, body := d.placementHold(t, alice, "a1"); code != http.StatusCreated {
		t.Fatalf("alice want 201 got %d %s", code, body)
	}
	if code, body := d.placementHold(t, bob, "b1"); code != http.StatusCreated {
		t.Fatalf("bob must execute independently: %d %s", code, body)
	}
	if code, body := d.placementHold(t, alice, "a2"); code != http.StatusConflict {
		t.Fatalf("alice second agent want 409 got %d %s", code, body)
	}
}

func proveAcquire(t *testing.T, d deployLab) {
	t.Helper()
	cust := d.cust("acq")
	if code, body := d.placementHold(t, cust, "p1"); code != http.StatusCreated {
		t.Fatalf("first acquire/hold want 201 got %d %s", code, body)
	}
	if code, body := d.placementHold(t, cust, "p2"); code != http.StatusConflict {
		t.Fatalf("second agent want 409 got %d %s", code, body)
	}
}

func proveRenew(t *testing.T, d deployLab) {
	t.Helper()
	cust, other := d.cust("ren"), d.cust("renx")
	tok := d.session(t, cust, "agent", "holder")
	got := d.acquire(t, tok)
	if got.Status != http.StatusCreated {
		t.Fatalf("acquire %d %s", got.Status, got.Raw)
	}
	renew := d.post(t, d.gw+"/v1/executions/"+got.ID+"/renew", "", map[string]string{"Authorization": "Bearer " + tok})
	if renew.Status != http.StatusOK {
		t.Fatalf("holder renew want 200 got %d %s", renew.Status, renew.Raw)
	}
	bob := d.session(t, other, "agent", "holder")
	cross := d.post(t, d.gw+"/v1/executions/"+got.ID+"/renew", "", map[string]string{"Authorization": "Bearer " + bob})
	if cross.Status == http.StatusOK {
		t.Fatalf("other customer must not renew: %s", cross.Raw)
	}
}

func proveRelease(t *testing.T, d deployLab) {
	t.Helper()
	cust := d.cust("rel")
	tok := d.session(t, cust, "agent", "p1")
	got := d.acquire(t, tok)
	if got.Status != http.StatusCreated {
		t.Fatalf("acquire %d %s", got.Status, got.Raw)
	}
	rel := d.post(t, d.gw+"/v1/executions/"+got.ID+"/release", "", map[string]string{"Authorization": "Bearer " + tok})
	if rel.Status != http.StatusOK {
		t.Fatalf("release %d %s", rel.Status, rel.Raw)
	}
	again := d.session(t, cust, "agent", "p2")
	got2 := d.acquire(t, again)
	if got2.Status != http.StatusCreated {
		t.Fatalf("reacquire after release want 201 got %d %s", got2.Status, got2.Raw)
	}
}

func proveQueue(t *testing.T, d deployLab) {
	t.Helper()
	d.putWaiting(t, 1)
	t.Cleanup(func() { d.putWaiting(t, 0) })

	cust := d.cust("que")
	t1 := d.session(t, cust, "agent", "q1")
	t2 := d.session(t, cust, "agent", "q2")
	t3 := d.session(t, cust, "agent", "q3")
	a := d.acquire(t, t1)
	if a.Status != http.StatusCreated {
		t.Fatalf("first %d %s", a.Status, a.Raw)
	}
	q := d.acquire(t, t2)
	if q.Status != http.StatusAccepted {
		t.Fatalf("second want QUEUED 202 got %d %s", q.Status, q.Raw)
	}
	b := d.acquire(t, t3)
	if b.Status != http.StatusConflict {
		t.Fatalf("third want BUSY 409 got %d %s", b.Status, b.Raw)
	}
	rel := d.post(t, d.gw+"/v1/executions/"+a.ID+"/release", "", map[string]string{"Authorization": "Bearer " + t1})
	if rel.Status != http.StatusOK {
		t.Fatalf("release %d %s", rel.Status, rel.Raw)
	}
	req, _ := http.NewRequest(http.MethodGet, d.gw+"/v1/executions/"+q.ID, nil)
	req.Header.Set("Authorization", "Bearer "+t2)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var promoted map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&promoted)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || promoted["status"] != "ACTIVE" {
		t.Fatalf("promoted %d %v", resp.StatusCode, promoted)
	}

	if !d.fronted() {
		return
	}
	frontCust := d.cust("quef")
	code, body, _ := d.holdFront(t, frontCust)
	if code != http.StatusCreated {
		t.Fatalf("front first want 201 got %d %s", code, body)
	}
	code, body, _ = d.holdFront(t, frontCust)
	if code != http.StatusAccepted {
		t.Fatalf("front second agent want QUEUED 202 got %d %s", code, body)
	}
}

func proveHandoff(t *testing.T, d deployLab) {
	t.Helper()
	cust := d.cust("han")
	agent := d.session(t, cust, "agent", "shopping-agent")
	browser := d.session(t, cust, "browser", "tab")
	got := d.acquire(t, agent)
	if got.Status != http.StatusCreated {
		t.Fatalf("agent acquire %d %s", got.Status, got.Raw)
	}
	var originFirst int
	if d.kind == kindEmbedded {
		originFirst = d.holdOrigin(t, got.Token)
		if originFirst != http.StatusCreated {
			t.Fatalf("agent origin hold %d", originFirst)
		}
	}
	busy := d.acquire(t, browser)
	if busy.Status != http.StatusConflict || !busy.CanPreempt {
		t.Fatalf("browser want 409 can_preempt: %d %s", busy.Status, busy.Raw)
	}
	hand := d.post(t, d.gw+"/v1/executions/"+got.ID+"/handoff", `{"mode":"preempt"}`, map[string]string{"Authorization": "Bearer " + browser})
	if hand.Status != http.StatusCreated {
		t.Fatalf("handoff %d %s", hand.Status, hand.Raw)
	}
	if hand.Fence <= got.Fence {
		t.Fatalf("fence %d → %d", got.Fence, hand.Fence)
	}
	renew := d.post(t, d.gw+"/v1/executions/"+got.ID+"/renew", "", map[string]string{"Authorization": "Bearer " + agent})
	if renew.Status != http.StatusGone {
		t.Fatalf("handed-off renew want 410 got %d %s", renew.Status, renew.Raw)
	}
	if d.kind != kindEmbedded {
		return
	}
	if code := d.holdOrigin(t, got.Token); code != http.StatusForbidden {
		t.Fatalf("stale fence want 403 got %d", code)
	}
	if code := d.holdOrigin(t, hand.Token); code != http.StatusCreated {
		t.Fatalf("successor origin hold %d", code)
	}
}

func proveRevoke(t *testing.T, d deployLab) {
	t.Helper()
	cust := d.cust("rev")
	agent := d.session(t, cust, "agent", "bot")
	browser := d.session(t, cust, "browser", "tab")
	got := d.acquire(t, agent)
	if got.Status != http.StatusCreated {
		t.Fatalf("acquire %d %s", got.Status, got.Raw)
	}
	rev := d.post(t, d.gw+"/v1/executions/"+got.ID+"/revoke", `{"reason":"customer_stop"}`, map[string]string{"Authorization": "Bearer " + browser})
	if rev.Status != http.StatusOK {
		t.Fatalf("revoke %d %s", rev.Status, rev.Raw)
	}
	again := d.acquire(t, browser)
	if again.Status != http.StatusCreated {
		t.Fatalf("reacquire after revoke want 201 got %d %s", again.Status, again.Raw)
	}
}

func proveExpiry(t *testing.T, d deployLab) {
	t.Helper()
	d.putControls(t, `{"lease_ttl_seconds":1,"updated_by":"deploy"}`)
	t.Cleanup(func() { d.putControls(t, `{"lease_ttl_seconds":0,"updated_by":"deploy"}`) })
	cust := d.cust("exp")
	tok := d.session(t, cust, "agent", "p1")
	got := d.acquire(t, tok)
	if got.Status != http.StatusCreated {
		t.Fatalf("acquire %d %s", got.Status, got.Raw)
	}
	time.Sleep(2 * time.Second)
	tok2 := d.session(t, cust, "agent", "p2")
	again := d.acquire(t, tok2)
	if again.Status != http.StatusCreated {
		t.Fatalf("acquire after expiry want 201 got %d %s", again.Status, again.Raw)
	}
	if again.ID == got.ID {
		t.Fatalf("expiry must grant a new execution, still %s", again.ID)
	}
}

func proveOriginLockdown(t *testing.T, d deployLab) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, d.origin+"/api/events/ars-che/holds", bytes.NewBufferString(`{"seats":1}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if d.kind == kindEmbedded {
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("embedded origin without token want 401 got %d %s", resp.StatusCode, b)
		}
		if code := d.holdOrigin(t, "not-a-jwt"); code != http.StatusUnauthorized {
			t.Fatalf("tampered token want 401 got %d", code)
		}
		return
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("locked origin without secret want 403 got %d %s", resp.StatusCode, b)
	}
	req, _ = http.NewRequest(http.MethodPost, d.origin+"/api/events/ars-che/holds", bytes.NewBufferString(`{"seats":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Bruiser-Origin-Secret", "spoofed-not-the-secret")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("spoofed origin secret want 403 got %d", resp.StatusCode)
	}
}

func proveDryRun(t *testing.T, d deployLab) {
	t.Helper()
	d.putControls(t, `{"enforce_percent":0,"enforcement":true,"mode":"dry-run","updated_by":"deploy"}`)
	t.Cleanup(func() { d.putControls(t, `{"enforce_percent":100,"enforcement":true,"mode":"enforce","updated_by":"deploy"}`) })
	cust := d.cust("dry")
	if d.fronted() {
		code, body, _ := d.holdFront(t, cust)
		if code != http.StatusCreated {
			t.Fatalf("0%% unaware hold must reach origin: %d %s", code, body)
		}
		code, body, _ = d.holdFront(t, cust)
		if code != http.StatusCreated {
			t.Fatalf("0%% second agent must still reach origin: %d %s", code, body)
		}
	} else {
		tok := d.session(t, cust, "agent", "d1")
		got := d.acquire(t, tok)
		if got.Status != http.StatusOK || got.Token != "" || got.Would == "" {
			t.Fatalf("embedded 0%% must ALLOW without a token: %d token=%q would=%q %s", got.Status, got.Token, got.Would, got.Raw)
		}
		tok2 := d.session(t, cust, "agent", "d2")
		got2 := d.acquire(t, tok2)
		if got2.Status != http.StatusOK || got2.Token != "" {
			t.Fatalf("embedded 0%% second agent must still observe: %d %s", got2.Status, got2.Raw)
		}
		if code := d.holdOrigin(t, ""); code != http.StatusUnauthorized {
			t.Fatalf("embedded origin stays locked without a token: %d", code)
		}
	}
	req, _ := http.NewRequest(http.MethodGet, d.gw+"/v1/admin/dry-run", nil)
	req.Header.Set("X-Bruiser-Admin-Secret", d.admin)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&report)
	resp.Body.Close()
	if report["requests_observed"].(float64) < 1 || report["requests_evaluated"].(float64) < 1 {
		t.Fatalf("dry-run must observe 100%% of seen traffic: %+v", report)
	}
}

func proveRamp10(t *testing.T, d deployLab) {
	t.Helper()
	inCust, outCust := d.rampPair(10)
	d.putControls(t, `{"enforce_percent":10,"enforcement":true,"mode":"enforce","updated_by":"deploy"}`)
	t.Cleanup(func() { d.putControls(t, `{"enforce_percent":100,"enforcement":true,"mode":"enforce","updated_by":"deploy"}`) })

	if code, body := d.placementHold(t, inCust, "in-1"); code != http.StatusCreated {
		t.Fatalf("10%% in-bucket first want 201 got %d %s", code, body)
	}
	if code, body := d.placementHold(t, inCust, "in-2"); code != http.StatusConflict && code != http.StatusAccepted {
		t.Fatalf("10%% in-bucket second want 409/202 got %d %s", code, body)
	}

	if d.fronted() {
		code, body, _ := d.holdFront(t, outCust)
		if code != http.StatusCreated {
			t.Fatalf("10%% out-bucket must observe (201): %d %s", code, body)
		}
		code, body, _ = d.holdFront(t, outCust)
		if code != http.StatusCreated {
			t.Fatalf("10%% out-bucket second agent must still observe: %d %s", code, body)
		}
		return
	}
	tok := d.session(t, outCust, "agent", "out-1")
	got := d.acquire(t, tok)
	if got.Status != http.StatusOK || got.Enforced == "1" || got.Token != "" {
		t.Fatalf("embedded 10%% out-bucket must observe without a token: %d enf=%s %s", got.Status, got.Enforced, got.Raw)
	}
	tok2 := d.session(t, outCust, "agent", "out-2")
	got2 := d.acquire(t, tok2)
	if got2.Status != http.StatusOK || got2.Token != "" {
		t.Fatalf("embedded 10%% out-bucket second agent: %d %s", got2.Status, got2.Raw)
	}
}

func proveEnforcement100(t *testing.T, d deployLab) {
	t.Helper()
	d.putControls(t, `{"enforce_percent":100,"enforcement":true,"mode":"enforce","updated_by":"deploy"}`)
	cust, other := d.cust("e100"), d.cust("e100b")
	if code, body := d.placementHold(t, cust, "p1"); code != http.StatusCreated {
		t.Fatalf("100%% first want 201 got %d %s", code, body)
	}
	if code, body := d.placementHold(t, cust, "p2"); code != http.StatusConflict {
		t.Fatalf("100%% second agent want 409 got %d %s", code, body)
	}
	if code, body := d.placementHold(t, other, "p1"); code != http.StatusCreated {
		t.Fatalf("100%% other customer want 201 got %d %s", code, body)
	}
}

func proveAuthority(t *testing.T, d deployLab) {
	t.Helper()
	if d.fronted() {
		rep, err := check.Run(check.Config{
			EdgeURL:    d.front,
			OriginURL:  d.origin,
			ControlURL: d.gw,
			HMACSecret: d.hmac,
			Membership: d.cust("authz"),
			EventID:    "ars-che",
		})
		if err != nil {
			t.Fatal(err)
		}
		if !rep.Passed() {
			t.Fatalf("authority-check must PASS for a locked %s install\n%s", d.kind, rep.String())
		}
		return
	}
	cust := d.cust("authz")
	if code := d.holdOrigin(t, ""); code != http.StatusUnauthorized {
		t.Fatalf("missing token %d", code)
	}
	if code := d.holdOrigin(t, "not-a-jwt"); code != http.StatusUnauthorized {
		t.Fatalf("tampered token %d", code)
	}
	tok := d.session(t, cust, "agent", "ok")
	got := d.acquire(t, tok)
	if got.Status != http.StatusCreated || got.Token == "" {
		t.Fatalf("acquire %d %s", got.Status, got.Raw)
	}
	if code := d.holdOrigin(t, got.Token); code != http.StatusCreated {
		t.Fatalf("legitimate embedded hold %d", code)
	}
	resp, err := http.Get(d.origin + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unlisted healthz %d", resp.StatusCode)
	}
}

func (d deployLab) fronted() bool { return d.kind != kindEmbedded }

func (d deployLab) cust(step string) string {
	return fmt.Sprintf("dep-%s-%s", d.kind, step)
}

func (d deployLab) rampPair(percent int) (in, out string) {
	in, out = d.cust("rin"), d.cust("rout")
	for i := 0; i < 500; i++ {
		id := fmt.Sprintf("%s-r%03d", d.kind, i)
		if ops.InRamp(id, percent, "") {
			in = id
		} else {
			out = id
		}
	}
	return in, out
}

func (d deployLab) boxCookie(t *testing.T, customer string) string {
	t.Helper()
	tok, err := auth.IssueBoxOfficeSession(d.hmac, customer, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func (d deployLab) session(t *testing.T, customer, ptype, principal string) string {
	t.Helper()
	assertion, err := auth.IssueDevAssertion(d.hmac, customer, time.Hour, nil)
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"principal":{"type":%q,"id":%q}}`, ptype, principal)
	req, _ := http.NewRequest(http.MethodPost, d.gw+"/v1/sessions", bytes.NewBufferString(body))
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
		t.Fatalf("session token: %s", raw)
	}
	return out.Token
}

func (d deployLab) acquire(t *testing.T, tok string) exeResult {
	t.Helper()
	return d.post(t, d.gw+"/v1/executions/acquire", `{"resource":"event:ars-che","action":"hold"}`, map[string]string{"Authorization": "Bearer " + tok})
}

func (d deployLab) placementHold(t *testing.T, customer, principal string) (int, string) {
	t.Helper()
	if d.fronted() {
		code, body, _ := d.holdFront(t, customer)
		return code, body
	}
	tok := d.session(t, customer, "agent", principal)
	got := d.acquire(t, tok)
	if got.Status != http.StatusCreated && got.Status != http.StatusOK {
		return got.Status, got.Raw
	}
	if got.Token == "" {
		return got.Status, got.Raw
	}
	return d.holdOrigin(t, got.Token), got.Raw
}

func (d deployLab) holdFront(t *testing.T, customer string) (int, string, http.Header) {
	t.Helper()
	return d.holdFrontRaw(t, d.boxCookie(t, customer), nil)
}

func (d deployLab) holdFrontRaw(t *testing.T, cookie string, extra map[string]string) (int, string, http.Header) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, d.front+"/api/events/ars-che/holds", bytes.NewBufferString(`{"seats":1}`))
	req.Header.Set("Content-Type", "application/json")
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: simtix.CookieName, Value: cookie})
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header.Clone()
}

func (d deployLab) holdOrigin(t *testing.T, exe string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, d.origin+"/api/events/ars-che/holds", bytes.NewBufferString(`{"seats":1}`))
	req.Header.Set("Content-Type", "application/json")
	if exe != "" {
		req.Header.Set("X-Bruiser-Execution", exe)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode
}

func (d deployLab) post(t *testing.T, url, body string, headers map[string]string) exeResult {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = bytes.NewBufferString(body)
	}
	req, _ := http.NewRequest(http.MethodPost, url, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := exeResult{Status: resp.StatusCode, Raw: string(raw), Header: resp.Header.Clone(), Enforced: resp.Header.Get("X-Bruiser-Enforced")}
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	if v, ok := m["execution_id"].(string); ok {
		out.ID = v
	}
	if v, ok := m["execution_token"].(string); ok {
		out.Token = v
	}
	if v, ok := m["fence"].(float64); ok {
		out.Fence = int64(v)
	}
	if v, ok := m["would"].(string); ok {
		out.Would = v
	}
	if v, ok := m["can_preempt"].(bool); ok {
		out.CanPreempt = v
	}
	if v, ok := m["reason"].(string); ok {
		out.Reason = v
	}
	if v, ok := m["successor_id"].(string); ok {
		out.SuccessorID = v
	}
	return out
}

func (d deployLab) putControls(t *testing.T, body string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, d.gw+"/v1/admin/controls", bytes.NewBufferString(body))
	req.Header.Set("X-Bruiser-Admin-Secret", d.admin)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put controls %d %s", resp.StatusCode, b)
	}
}

func (d deployLab) putWaiting(t *testing.T, maxWaiters int) {
	t.Helper()
	doc := policy.DefaultDocument(d.merchant)
	if maxWaiters > 0 {
		doc.Domains[0].Waiting = policy.Waiting{Mode: "bounded", MaxWaiters: maxWaiters}
	}
	raw, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodPut, d.gw+"/v1/policy", bytes.NewReader(raw))
	req.Header.Set("X-Bruiser-Admin-Secret", d.admin)
	req.Header.Set("Content-Type", "application/yaml")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("put policy %d %s", resp.StatusCode, b)
	}
}
