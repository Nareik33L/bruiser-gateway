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

func startLab(t *testing.T, originSecret string) (edgeURL, originURL string, hmac string) {
	t.Helper()
	gw, cfg := testlab.Gateway(t, testlab.ArsenalProfile(t))
	origin := simtix.New(simtix.Config{
		HMACSecret:   cfg.DevHMACSecret,
		OriginSecret: originSecret,
		Seats:        20,
	})
	originSrv := httptest.NewServer(origin.Handler())
	t.Cleanup(originSrv.Close)
	p, err := edge.New(edge.Config{
		OriginURL:   originSrv.URL,
		BruiserURL:  gw.URL,
		EdgeSecret:  cfg.EdgeSecret,
		MaxInFlight: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	edgeSrv := httptest.NewServer(p.Handler())
	t.Cleanup(edgeSrv.Close)
	return edgeSrv.URL, originSrv.URL, cfg.DevHMACSecret
}

func TestAuthorityCheckLockdownOnPass(t *testing.T) {
	edgeURL, originURL, hmac := startLab(t, "origin-lock-dev")
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
	if !rep.Passed() {
		t.Fatalf("want PASS\n%s", rep.String())
	}
	if !strings.Contains(rep.String(), "Overall Result: PASS") {
		t.Fatalf("format:\n%s", rep.String())
	}
}

func TestAuthorityCheckLockdownOffFail(t *testing.T) {
	edgeURL, originURL, hmac := startLab(t, "")
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
		t.Fatalf("lockdown off must FAIL\n%s", rep.String())
	}
	var bypass check.Probe
	for _, p := range rep.Probes {
		if p.Name == "Direct allocation bypass blocked" {
			bypass = p
		}
	}
	if bypass.Pass {
		t.Fatalf("bypass probe should fail when lockdown is off: %+v", bypass)
	}
}

func TestUnawareSwarmOneHold(t *testing.T) {
	edgeURL, _, hmac := startLab(t, "origin-lock-dev")
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

func TestSearchOpenViaEdge(t *testing.T) {
	edgeURL, _, _ := startLab(t, "origin-lock-dev")
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
