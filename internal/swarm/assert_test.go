package swarm_test

import (
	"os"
	"testing"

	"github.com/Nareik33L/bruiser-gateway/internal/check"
	"github.com/Nareik33L/bruiser-gateway/internal/swarm"
	"github.com/Nareik33L/bruiser-gateway/internal/testlab"
	"net/http/httptest"

	"github.com/Nareik33L/bruiser-gateway/internal/simtix"
)

func TestDemoAssertion(t *testing.T) {
	if os.Getenv("BRUISER_TEST_DATABASE_URL") == "" {
		t.Skip("BRUISER_TEST_DATABASE_URL not set")
	}
	api, gw, cfg, _ := testlab.GatewayAPI(t, testlab.ArsenalProfile(t))
	origin := simtix.New(simtix.Config{
		HMACSecret:   cfg.DevHMACSecret,
		OriginSecret: cfg.OriginSecret,
		Seats:        400,
	})
	originSrv := httptest.NewServer(origin.Handler())
	t.Cleanup(originSrv.Close)
	ph, err := api.ProxyHandler(originSrv.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(ph)
	t.Cleanup(proxy.Close)

	res, err := swarm.Run(swarm.Config{
		Front:      proxy.URL,
		HMACSecret: cfg.DevHMACSecret,
		N:          50,
	}, swarm.Profile1xN)
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Allow != 1 || res.Busy != 49 {
		t.Fatalf("1x50 %+v", res)
	}
	if res.ObservedEAF < 40 || res.DownstreamEAF != 1 {
		t.Fatalf("eaf observed=%.0f downstream=%.0f", res.ObservedEAF, res.DownstreamEAF)
	}

	nxk, err := swarm.Run(swarm.Config{
		Front:      proxy.URL,
		HMACSecret: cfg.DevHMACSecret,
		Customers:  8,
		PerCust:    3,
	}, swarm.ProfileNxK)
	if err != nil {
		t.Fatal(err)
	}
	if !nxk.OK {
		t.Fatalf("NxK %+v", nxk)
	}

	rep, err := check.Run(check.Config{
		EdgeURL:    proxy.URL,
		OriginURL:  originSrv.URL,
		HMACSecret: cfg.DevHMACSecret,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Passed() {
		t.Fatalf("authority check\n%s", rep.String())
	}
	_ = gw
}
