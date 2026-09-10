// Command swarm runs demo load profiles against an Edge or Proxy front.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Nareik33L/bruiser-gateway/internal/swarm"
)

func main() {
	front := flag.String("front", env("SIMTIX_EDGE_URL", "http://127.0.0.1:8091"), "Edge or Proxy URL")
	origin := flag.String("origin", env("SIMTIX_ORIGIN_URL", "http://127.0.0.1:8090"), "origin URL (bypass)")
	secret := flag.String("hmac-secret", env("BRUISER_DEV_HMAC_SECRET", ""), "box-office HMAC")
	profile := flag.String("profile", "1xN", "1xN | NxK | unaware | bypass | handoff")
	n := flag.Int("n", 200, "agents for 1xN / unaware / bypass")
	customers := flag.Int("customers", 20, "customers for NxK")
	per := flag.Int("per-customer", 3, "agents per customer for NxK")
	flag.Parse()
	res, err := swarm.Run(swarm.Config{
		Front:      *front,
		Origin:     *origin,
		HMACSecret: *secret,
		N:          *n,
		Customers:  *customers,
		PerCust:    *per,
	}, swarm.Profile(*profile))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(string(res.JSON()))
	if !res.OK {
		os.Exit(1)
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
