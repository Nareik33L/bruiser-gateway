package check_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/check"
	"github.com/Nareik33L/bruiser-gateway/internal/edge"
	"github.com/Nareik33L/bruiser-gateway/internal/simtix"
	"github.com/Nareik33L/bruiser-gateway/internal/testlab"
)

func startLab(t *testing.T, originSecret string) (edgeURL, originURL, gwURL, hmac string) {
	t.Helper()
	_, gw, cfg, signer := testlab.GatewayAPI(t, testlab.ArsenalProfile(t))
	origin := simtix.New(simtix.Lab(cfg.DevHMACSecret, originSecret, cfg.MerchantID, signer.Public, gw.URL, 20))
	originSrv := httptest.NewServer(origin.Handler())
	t.Cleanup(originSrv.Close)
	p, err := edge.New(edge.Config{
		OriginURL:    originSrv.URL,
		BruiserURL:   gw.URL,
		EdgeSecret:   cfg.EdgeSecret,
		OriginSecret: originSecret,
		MaxInFlight:  8,
	})
	if err != nil {
		t.Fatal(err)
	}
	edgeSrv := httptest.NewServer(p.Handler())
	t.Cleanup(edgeSrv.Close)
	return edgeSrv.URL, originSrv.URL, gw.URL, cfg.DevHMACSecret
}

func TestAuthorityCheckLockdownOnPass(t *testing.T) {
	edgeURL, originURL, gwURL, hmac := startLab(t, "origin-lock-dev")
	rep, err := check.Run(check.Config{
		EdgeURL:    edgeURL,
		OriginURL:  originURL,
		ControlURL: gwURL,
		HMACSecret: hmac,
		Membership: "1001234",
		EventID:    "ars-che",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Passed() {
		t.Fatalf("want PASS\n%s", rep.String())
	}
	if !strings.Contains(rep.String(), "Overall Result: PASS") {
		t.Fatalf("format:\n%s", rep.String())
	}
}

func TestAuthorityCheckLockdownOffFail(t *testing.T) {
	edgeURL, originURL, gwURL, hmac := startLab(t, "")
	rep, err := check.Run(check.Config{
		EdgeURL:    edgeURL,
		OriginURL:  originURL,
		ControlURL: gwURL,
		HMACSecret: hmac,
		Membership: "1001234",
		EventID:    "ars-che",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Passed() {
		t.Fatalf("lockdown off must FAIL\n%s", rep.String())
	}
	var pathTrust check.Probe
	for _, p := range rep.Probes {
		if p.Name == "Origin secret required with a valid execution" {
			pathTrust = p
		}
	}
	if pathTrust.Pass {
		t.Fatalf("path-trust probe should fail when lockdown is off: %+v", pathTrust)
	}
}

func TestUnawareSwarmOneHold(t *testing.T) {
	edgeURL, _, _, hmac := startLab(t, "origin-lock-dev")
	c1, err := auth.IssueBoxOfficeSession(hmac, "1001234", 0)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := auth.IssueBoxOfficeSession(hmac, "1001234", 0)
	if err != nil {
		t.Fatal(err)
	}
	hold := func(cookie string) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, edgeURL+"/api/events/ars-che/holds", bytes.NewBufferString(`{"seats":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: simtix.CookieName, Value: cookie})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}
	if code := hold(c1); code != http.StatusCreated {
		t.Fatalf("first unaware hold %d", code)
	}
	if code := hold(c1); code != http.StatusCreated {
		t.Fatalf("same cookie second hold (already held) %d", code)
	}
	if code := hold(c2); code != http.StatusConflict {
		t.Fatalf("second membership session want BUSY 409 got %d", code)
	}
}

func TestAuthorityCheckMissingOriginFails(t *testing.T) {
	edgeURL, _, gwURL, hmac := startLab(t, "origin-lock-dev")
	rep, err := check.Run(check.Config{
		EdgeURL:    edgeURL,
		ControlURL: gwURL,
		HMACSecret: hmac,
		Membership: "1001234",
		EventID:    "ars-che",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Passed() || rep.Overall != "FAIL" {
		t.Fatalf("missing --origin must FAIL, not WARN/PASS\n%s", rep.String())
	}
	var originProbe check.Probe
	for _, p := range rep.Probes {
		if p.Name == "Origin lockdown proven" {
			originProbe = p
		}
	}
	if originProbe.Status != "FAIL" {
		t.Fatalf("origin required probe: %+v\n%s", originProbe, rep.String())
	}
}

func TestAuthorityCheckMissingControlFails(t *testing.T) {
	edgeURL, originURL, _, hmac := startLab(t, "origin-lock-dev")
	rep, err := check.Run(check.Config{
		EdgeURL:    edgeURL,
		OriginURL:  originURL,
		HMACSecret: hmac,
		Membership: "1001234",
		EventID:    "ars-che",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Passed() {
		t.Fatalf("missing --control must FAIL\n%s", rep.String())
	}
}

func TestSearchOpenViaEdge(t *testing.T) {
	edgeURL, _, _, _ := startLab(t, "origin-lock-dev")
	resp, err := http.Get(edgeURL + "/api/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("search %d %s", resp.StatusCode, b)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if _, ok := out["events"]; !ok {
		t.Fatalf("%v", out)
	}
}
