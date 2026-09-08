package check_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/check"
	"github.com/Nareik33L/bruiser-gateway/internal/simtix"
	"github.com/Nareik33L/bruiser-gateway/internal/testlab"
)

func startProxyLab(t *testing.T, originSecret string) (proxyURL, originURL, gwURL, hmac string) {
	t.Helper()
	api, gw, cfg, _ := testlab.GatewayAPI(t, testlab.ArsenalProfile(t))
	origin := simtix.New(simtix.Config{
		HMACSecret:   cfg.DevHMACSecret,
		OriginSecret: originSecret,
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
	return proxySrv.URL, originSrv.URL, gw.URL, cfg.DevHMACSecret
}

func TestAuthorityCheckProxyLockdownOnPass(t *testing.T) {
	front, origin, gw, hmac := startProxyLab(t, "origin-lock-dev")
	rep, err := check.Run(check.Config{
		EdgeURL:    front,
		OriginURL:  origin,
		ControlURL: gw,
		HMACSecret: hmac,
		Membership: "1001234",
		EventID:    "ars-che",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Passed() {
		t.Fatalf("proxy want PASS\n%s", rep.String())
	}
}

func TestProxyUnawareBusy(t *testing.T) {
	front, _, _, hmac := startProxyLab(t, "origin-lock-dev")
	c1, _ := auth.IssueBoxOfficeSession(hmac, "1001234", 0)
	c2, _ := auth.IssueBoxOfficeSession(hmac, "1001234", 0)
	hold := func(c string) int {
		req, _ := http.NewRequest(http.MethodPost, front+"/api/events/ars-che/holds", bytes.NewBufferString(`{"seats":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: simtix.CookieName, Value: c})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}
	if hold(c1) != http.StatusCreated {
		t.Fatal("first")
	}
	if hold(c2) != http.StatusConflict {
		t.Fatal("second want BUSY")
	}
}

func TestEAFUnawareSwarm(t *testing.T) {
	front, origin, gw, hmac := startProxyLab(t, "origin-lock-dev")
	const n = 200
	var allow, busy atomic.Int64
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			tok, err := auth.IssueBoxOfficeSession(hmac, "1001234", 0)
			if err != nil {
				t.Error(err)
				return
			}
			req, _ := http.NewRequest(http.MethodPost, front+"/api/events/ars-che/holds", bytes.NewBufferString(`{"seats":1}`))
			req.Header.Set("Content-Type", "application/json")
			req.AddCookie(&http.Cookie{Name: simtix.CookieName, Value: tok})
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Error(err)
				return
			}
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			switch resp.StatusCode {
			case http.StatusCreated:
				allow.Add(1)
			case http.StatusConflict:
				busy.Add(1)
			default:
				t.Errorf("status %d", resp.StatusCode)
			}
		}()
	}
	wg.Wait()
	if allow.Load() != 1 || busy.Load() != n-1 {
		t.Fatalf("allow=%d busy=%d want 1/%d", allow.Load(), busy.Load(), n-1)
	}
	resp, err := http.Get(origin + "/api/events/ars-che")
	if err != nil {
		t.Fatal(err)
	}
	var ev struct {
		Held      int `json:"held"`
		Available int `json:"available"`
		Seats     int `json:"seats"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ev); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if ev.Held != 1 || ev.Available != ev.Seats-1 {
		t.Fatalf("origin held=%d available=%d seats=%d — swarm must not multiply origin holds", ev.Held, ev.Available, ev.Seats)
	}
	resp, err = http.Get(gw + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	metrics := string(b)
	if !bytes.Contains(b, []byte("bruiser_observed_eaf")) {
		t.Fatalf("missing observed_eaf in metrics:\n%s", metrics)
	}
}

func TestEmbeddedRejectsMissingAndTampered(t *testing.T) {
	api, gw, cfg, signer := testlab.GatewayAPI(t, testlab.ArsenalProfile(t))
	_ = api
	origin := simtix.New(simtix.Config{
		HMACSecret:       cfg.DevHMACSecret,
		RequireExecution: true,
		VerifyExecution: func(token string) error {
			_, err := auth.ParseExecution(token, signer.Public)
			return err
		},
		Seats: 10,
	})
	originSrv := httptest.NewServer(origin.Handler())
	t.Cleanup(originSrv.Close)

	hold := func(exe string) int {
		req, _ := http.NewRequest(http.MethodPost, originSrv.URL+"/api/events/ars-che/holds", bytes.NewBufferString(`{"seats":1}`))
		req.Header.Set("Content-Type", "application/json")
		if exe != "" {
			req.Header.Set("X-Bruiser-Execution", exe)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}
	if hold("") != http.StatusUnauthorized {
		t.Fatal("missing token")
	}
	if hold("not-a-jwt") != http.StatusUnauthorized {
		t.Fatal("tampered")
	}

	assertion, err := auth.IssueBoxOfficeSession(cfg.DevHMACSecret, "1001234", 0)
	if err != nil {
		t.Fatal(err)
	}
	sessBody := `{"principal":{"type":"agent","id":"embedded-1"}}`
	req, _ := http.NewRequest(http.MethodPost, gw.URL+"/v1/sessions", bytes.NewBufferString(sessBody))
	req.Header.Set("Authorization", "Bearer "+assertion)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var sess struct {
		Token string `json:"session_token"`
	}
	json.NewDecoder(resp.Body).Decode(&sess)
	resp.Body.Close()
	if sess.Token == "" {
		t.Fatal("session")
	}
	req, _ = http.NewRequest(http.MethodPost, gw.URL+"/v1/executions/acquire", bytes.NewBufferString(`{"resource":"event:ars-che","action":"hold"}`))
	req.Header.Set("Authorization", "Bearer "+sess.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var acq struct {
		Token string `json:"execution_token"`
	}
	json.NewDecoder(resp.Body).Decode(&acq)
	resp.Body.Close()
	if acq.Token == "" {
		t.Fatal("execution token")
	}
	if hold(acq.Token) != http.StatusCreated {
		t.Fatal("valid embedded hold")
	}

	req, _ = http.NewRequest(http.MethodPost, gw.URL+"/v1/introspect", nil)
	req.Header.Set("Authorization", "Bearer "+acq.Token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("introspect %d", resp.StatusCode)
	}
}
