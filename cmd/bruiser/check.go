package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/Nareik33L/bruiser-gateway/internal/check"
)

func cmdAuthorityCheck() error {
	fs := flag.NewFlagSet("authority-check", flag.ContinueOnError)
	edgeURL := fs.String("edge", env("SIMTIX_EDGE_URL", "http://127.0.0.1:8091"), "Edge URL (WAF analogue)")
	originURL := fs.String("origin", env("SIMTIX_ORIGIN_URL", "http://127.0.0.1:8090"), "box-office origin URL")
	secret := fs.String("hmac-secret", env("BRUISER_DEV_HMAC_SECRET", "dev-secret-change-me"), "HMAC used to mint box-office cookies")
	membership := fs.String("membership", "1001234", "7-digit membership number analogue")
	eventID := fs.String("event", "ars-che", "event id")
	asJSON := fs.Bool("json", false, "print the report as JSON")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}
	rep, err := check.Run(check.Config{
		EdgeURL:    *edgeURL,
		OriginURL:  *originURL,
		HMACSecret: *secret,
		Membership: *membership,
		EventID:    *eventID,
	})
	if err != nil {
		return err
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			return err
		}
	} else {
		fmt.Print(rep.String())
	}
	if !rep.Passed() {
		return fmt.Errorf("authority check FAIL")
	}
	return nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
