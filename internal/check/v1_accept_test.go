package check_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/check"
	"github.com/Nareik33L/bruiser-gateway/internal/edge"
	"github.com/Nareik33L/bruiser-gateway/internal/ops"
	"github.com/Nareik33L/bruiser-gateway/internal/simtix"
	"github.com/Nareik33L/bruiser-gateway/internal/testlab"
)

type proxyLab struct {
	front  string
	origin string
	gw     string
	hmac   string
	admin  string
	edge   string
}

func startProxyLabFull(t *testing.T) proxyLab {
	t.Helper()
	api, gw, cfg, _ := testlab.GatewayAPI(t, testlab.ArsenalProfile(t))
	origin := simtix.New(simtix.Config{
		HMACSecret:   cfg.DevHMACSecret,
		OriginSecret: cfg.OriginSecret,
		Seats:        200,
	})
	originSrv := httptest.NewServer(origin.Handler())
	t.Cleanup(originSrv.Close)
	ph, err := api.ProxyHandler(originSrv.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxySrv := httptest.NewServer(ph)
	t.Cleanup(proxySrv.Close)
	return proxyLab{
		front:  proxySrv.URL,
		origin: originSrv.URL,
		gw:     gw.URL,
		hmac:   cfg.DevHMACSecret,
		admin:  cfg.AdminSecret,
		edge:   cfg.EdgeSecret,
	}
}

func (l proxyLab) cookie(t *testing.T, customer string) string {
	t.Helper()
	tok, err := auth.IssueBoxOfficeSession(l.hmac, customer, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func (l proxyLab) hold(t *testing.T, customer string) (int, string, http.Header) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, l.front+"/api/events/ars-che/holds", bytes.NewBufferString(`{"seats":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: simtix.CookieName, Value: l.cookie(t, customer)})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b), resp.Header.Clone()
}

func (l proxyLab) originEvent(t *testing.T) (held, available, seats int) {
	t.Helper()
	resp, err := http.Get(l.origin + "/api/events/ars-che")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var ev struct {
		Held      int `json:"held"`
		Available int `json:"available"`
		Seats     int `json:"seats"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ev); err != nil {
		t.Fatal(err)
	}
	return ev.Held, ev.Available, ev.Seats
}

func (l proxyLab) putControls(t *testing.T, body string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPut, l.gw+"/v1/admin/controls", bytes.NewBufferString(body))
	req.Header.Set("X-Bruiser-Admin-Secret", l.admin)
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

func (l proxyLab) authorize(t *testing.T, customer string) (int, http.Header, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, l.gw+"/v1/authorize", bytes.NewBufferString(`{"method":"POST","path":"/api/events/ars-che/holds"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Bruiser-Edge-Secret", l.edge)
	req.Header.Set("Cookie", simtix.CookieName+"="+l.cookie(t, customer))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, resp.Header.Clone(), out
}

func (l proxyLab) adminStatus(t *testing.T) map[string]any {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, l.gw+"/v1/admin/status", nil)
	req.Header.Set("X-Bruiser-Admin-Secret", l.admin)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestV1AcceptanceLifecycle proves the complete customer → proxy → origin path
// for one vs many agents, independent customers, cancel, and origin hold count.
func TestV1AcceptanceLifecycle(t *testing.T) {
	lab := startProxyLabFull(t)

	code, body, _ := lab.hold(t, "1001234")
	if code != http.StatusCreated {
		t.Fatalf("one customer + one agent want 201 got %d %s", code, body)
	}
	held, available, seats := lab.originEvent(t)
	if held != 1 || available != seats-1 {
		t.Fatalf("origin after first hold held=%d available=%d seats=%d", held, available, seats)
	}

	code, body, _ = lab.hold(t, "1001234")
	if code != http.StatusConflict {
		t.Fatalf("same customer + second agent want 409 got %d %s", code, body)
	}
	held, _, _ = lab.originEvent(t)
	if held != 1 {
		t.Fatalf("second agent must not create another origin hold: held=%d", held)
	}

	code, body, _ = lab.hold(t, "2000001")
	if code != http.StatusCreated {
		t.Fatalf("different customer must execute independently: %d %s", code, body)
	}
	held, _, _ = lab.originEvent(t)
	if held != 2 {
		t.Fatalf("two customers should hold two seats: held=%d", held)
	}

	st := lab.adminStatus(t)
	eaf, _ := st["eaf"].(map[string]any)
	if eaf["attempts"].(float64) < 3 || eaf["forwarded"].(float64) != 2 {
		t.Fatalf("admin eaf attempts/forwarded = %+v want >=3 / 2", eaf)
	}
}

func TestV1AcceptanceAmplificationAndIndependence(t *testing.T) {
	lab := startProxyLabFull(t)
	const agents = 250
	const customers = 3
	var allow, busy, other atomic.Int64
	var wg sync.WaitGroup
	wg.Add(agents * customers)
	for c := 0; c < customers; c++ {
		cust := fmt.Sprintf("amp-%d", c)
		for i := 0; i < agents; i++ {
			go func(customer string) {
				defer wg.Done()
				code, _, _ := lab.hold(t, customer)
				switch code {
				case http.StatusCreated:
					allow.Add(1)
				case http.StatusConflict:
					busy.Add(1)
				default:
					other.Add(1)
				}
			}(cust)
		}
	}
	wg.Wait()
	if other.Load() != 0 {
		t.Fatalf("unexpected statuses: other=%d", other.Load())
	}
	if allow.Load() != int64(customers) {
		t.Fatalf("authorised executions=%d want %d (one per customer)", allow.Load(), customers)
	}
	if busy.Load() != int64(agents*customers-customers) {
		t.Fatalf("busy=%d want %d", busy.Load(), agents*customers-customers)
	}
	held, _, _ := lab.originEvent(t)
	if held != customers {
		t.Fatalf("origin holds=%d want %d — agent multiplication must not multiply origin executions", held, customers)
	}

	resp, err := http.Get(lab.gw + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	metrics, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	text := string(metrics)
	for _, needle := range []string{
		"bruiser_allocation_attempts_total",
		"bruiser_executions_forwarded_total",
		"bruiser_observed_eaf",
		"bruiser_downstream_eaf",
	} {
		if !strings.Contains(text, needle) {
			t.Fatalf("metrics missing %s", needle)
		}
	}
}

func TestV1AcceptanceRampSemantics(t *testing.T) {
	lab := startProxyLabFull(t)

	inCust, outCust := "ramp-in", "ramp-out"
	for i := 0; i < 500; i++ {
		id := fmt.Sprintf("r%03d", i)
		if ops.InRamp(id, 10, "") {
			inCust = id
		} else {
			outCust = id
		}
	}
	if ops.InRamp(inCust, 10, "") == ops.InRamp(outCust, 10, "") {
		t.Fatal("need one in-bucket and one out-bucket customer")
	}

	lab.putControls(t, `{"enforce_percent":0,"enforcement":true,"mode":"dry-run","updated_by":"v1"}`)
	code, hdr, body := lab.authorize(t, inCust)
	if code != 200 || hdr.Get("X-Bruiser-Enforced") != "0" || body["status"] != "ALLOW" {
		t.Fatalf("0%% must observe not block: %d enf=%s %v", code, hdr.Get("X-Bruiser-Enforced"), body)
	}
	if body["would"] == nil {
		t.Fatalf("0%% must record hypothetical: %v", body)
	}
	code2, _, body2 := lab.authorize(t, inCust)
	if code2 != 200 || body2["status"] != "ALLOW" {
		t.Fatalf("0%% second agent must still ALLOW: %d %v", code2, body2)
	}

	lab.putControls(t, `{"enforce_percent":10,"enforcement":true,"mode":"enforce","updated_by":"v1"}`)
	code, hdr, body = lab.authorize(t, inCust)
	if code != 200 || hdr.Get("X-Bruiser-Enforced") != "1" {
		t.Fatalf("10%% in-bucket must enforce: %d enf=%s %v", code, hdr.Get("X-Bruiser-Enforced"), body)
	}
	code, hdr, body = lab.authorize(t, inCust)
	if code != http.StatusAccepted && code != http.StatusConflict {
		t.Fatalf("10%% in-bucket second agent want QUEUED/BUSY got %d %v", code, body)
	}
	if hdr.Get("X-Bruiser-Enforced") == "0" {
		t.Fatal("in-bucket agents must inherit the same cohort")
	}

	var would string
	for i := 0; i < 5; i++ {
		code, hdr, body = lab.authorize(t, outCust)
		if code != 200 || hdr.Get("X-Bruiser-Enforced") != "0" || body["status"] != "ALLOW" {
			t.Fatalf("10%% out-bucket must observe: %d enf=%s %v", code, hdr.Get("X-Bruiser-Enforced"), body)
		}
		if body["would"] == nil {
			t.Fatalf("90%% unenforced must still record hypothetical: %v", body)
		}
		if would == "" {
			would = fmt.Sprint(body["would"])
		}
	}

	lab.putControls(t, `{"enforce_percent":100,"enforcement":true,"mode":"enforce","updated_by":"v1"}`)
	code, hdr, body = lab.authorize(t, outCust)
	if hdr.Get("X-Bruiser-Enforced") != "1" {
		t.Fatalf("100%% must enforce previously unenforced customer: enf=%s %v", hdr.Get("X-Bruiser-Enforced"), body)
	}
	_ = code

	req, _ := http.NewRequest(http.MethodGet, lab.gw+"/v1/admin/dry-run", nil)
	req.Header.Set("X-Bruiser-Admin-Secret", lab.admin)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&report)
	resp.Body.Close()
	if report["requests_observed"].(float64) < 1 || report["requests_evaluated"].(float64) < 1 {
		t.Fatalf("observation must be 100%% of seen traffic: %+v", report)
	}
}

func TestV1AcceptanceAuthorityAndControlPlane(t *testing.T) {
	lab := startProxyLabFull(t)
	rep, err := check.Run(check.Config{
		EdgeURL:    lab.front,
		OriginURL:  lab.origin,
		ControlURL: lab.gw,
		HMACSecret: lab.hmac,
		Membership: "1009999",
		EventID:    "ars-che",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Passed() {
		t.Fatalf("authority check must PASS for a locked Proxy deployment\n%s", rep.String())
	}
	need := map[string]bool{
		"Spoofed identity headers rejected":               false,
		"Unlisted allocation paths cannot grant":          false,
		"Authorize requires edge secret":                  false,
		"Direct allocation bypass blocked":                false,
		"Spoofed origin secret rejected":                  false,
		"Origin rejects missing Bruiser credentials":      false,
		"Legitimate Bruiser-mediated allocation succeeds": false,
	}
	for _, p := range rep.Probes {
		if _, ok := need[p.Name]; ok && p.Status == "PASS" {
			need[p.Name] = true
		}
	}
	for name, ok := range need {
		if !ok {
			t.Fatalf("missing PASS probe %q\n%s", name, rep.String())
		}
	}
}

func TestV1AcceptancePlacementParity(t *testing.T) {
	// Proxy and Edge must apply the same one-customer / second-agent rule.
	proxy := startProxyLabFull(t)
	c1 := proxy.cookie(t, "parity-a")
	c2 := proxy.cookie(t, "parity-a")
	hold := func(front, tok string) int {
		req, _ := http.NewRequest(http.MethodPost, front+"/api/events/ars-che/holds", bytes.NewBufferString(`{"seats":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: simtix.CookieName, Value: tok})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp.StatusCode
	}
	if hold(proxy.front, c1) != http.StatusCreated {
		t.Fatal("proxy first")
	}
	if hold(proxy.front, c2) != http.StatusConflict {
		t.Fatal("proxy second")
	}

	gw, cfg := testlab.Gateway(t, testlab.ArsenalProfile(t))
	origin := simtix.New(simtix.Config{HMACSecret: cfg.DevHMACSecret, OriginSecret: cfg.OriginSecret, Seats: 20})
	originSrv := httptest.NewServer(origin.Handler())
	t.Cleanup(originSrv.Close)
	p, err := edge.New(edge.Config{OriginURL: originSrv.URL, BruiserURL: gw.URL, EdgeSecret: cfg.EdgeSecret, MaxInFlight: 8})
	if err != nil {
		t.Fatal(err)
	}
	edgeSrv := httptest.NewServer(p.Handler())
	t.Cleanup(edgeSrv.Close)
	e1, _ := auth.IssueBoxOfficeSession(cfg.DevHMACSecret, "parity-b", time.Hour)
	e2, _ := auth.IssueBoxOfficeSession(cfg.DevHMACSecret, "parity-b", time.Hour)
	if hold(edgeSrv.URL, e1) != http.StatusCreated {
		t.Fatal("edge first")
	}
	if hold(edgeSrv.URL, e2) != http.StatusConflict {
		t.Fatal("edge second")
	}
}

func TestV1AcceptanceEmergencyZeroKeepsObservation(t *testing.T) {
	lab := startProxyLabFull(t)
	lab.putControls(t, `{"enforce_percent":0,"updated_by":"emergency"}`)
	code, _, _ := lab.hold(t, "emerg-1")
	if code != http.StatusCreated {
		t.Fatalf("emergency 0%% must not block unaware hold: %d", code)
	}
	code, _, _ = lab.hold(t, "emerg-1")
	if code != http.StatusCreated {
		t.Fatalf("emergency 0%% second agent must still reach origin: %d", code)
	}
	st := lab.adminStatus(t)
	dr, _ := st["dry_run"].(map[string]any)
	if dr == nil || dr["requests_observed"].(float64) < 1 {
		t.Fatalf("emergency 0%% must still record observation: %+v", st)
	}
}
