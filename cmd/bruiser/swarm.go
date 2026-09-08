package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Nareik33L/bruiser-gateway/internal/swarm"
)

func cmdSwarm() error {
	fs := flag.NewFlagSet("swarm", flag.ContinueOnError)
	front := fs.String("front", env("SIMTIX_EDGE_URL", "http://127.0.0.1:8091"), "Edge or Proxy URL")
	origin := fs.String("origin", env("SIMTIX_ORIGIN_URL", "http://127.0.0.1:8090"), "origin URL (bypass)")
	secret := fs.String("hmac-secret", env("BRUISER_DEV_HMAC_SECRET", "dev-secret-change-me"), "box-office HMAC")
	profile := fs.String("profile", "1xN", "1xN | NxK | unaware | bypass | handoff")
	n := fs.Int("n", 200, "agents for 1xN / unaware / bypass")
	customers := fs.Int("customers", 20, "customers for NxK")
	per := fs.Int("per-customer", 3, "agents per customer for NxK")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}
	res, err := swarm.Run(swarm.Config{
		Front:      *front,
		Origin:     *origin,
		HMACSecret: *secret,
		N:          *n,
		Customers:  *customers,
		PerCust:    *per,
	}, swarm.Profile(*profile))
	if err != nil {
		return err
	}
	fmt.Println(string(res.JSON()))
	if !res.OK {
		return fmt.Errorf("swarm profile %s failed allow=%d busy=%d other=%d", res.Profile, res.Allow, res.Busy, res.Other)
	}
	return nil
}
