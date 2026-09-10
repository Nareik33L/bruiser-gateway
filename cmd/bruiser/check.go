package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/check"
	"github.com/Nareik33L/bruiser-gateway/internal/config"
	pgstore "github.com/Nareik33L/bruiser-gateway/internal/store/postgres"
)

func cmdAuthorityCheck() error {
	fs := flag.NewFlagSet("authority-check", flag.ContinueOnError)
	edgeURL := fs.String("edge", env("SIMTIX_EDGE_URL", "http://127.0.0.1:8091"), "Edge or Proxy URL (enforcement front)")
	frontURL := fs.String("front", "", "alias of -edge")
	originURL := fs.String("origin", env("SIMTIX_ORIGIN_URL", "http://127.0.0.1:8090"), "box-office origin URL")
	controlURL := fs.String("control", env("BRUISER_HTTP_URL", ""), "optional Bruiser control-plane URL for /v1/authorize probes")
	adminURL := fs.String("admin", env("BRUISER_ADMIN_URL", ""), "Bruiser admin listener URL (policy/admin); not the public control URL")
	secret := fs.String("hmac-secret", env("BRUISER_DEV_HMAC_SECRET", ""), "HMAC used to mint box-office cookies")
	membership := fs.String("membership", "1001234", "7-digit membership number analogue")
	eventID := fs.String("event", "ars-che", "event id")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}
	front := *edgeURL
	if *frontURL != "" {
		front = *frontURL
	}
	if strings.TrimSpace(*originURL) == "" {
		return fmt.Errorf("authority-check requires --origin (do not go live without proving origin lockdown)")
	}
	if strings.TrimSpace(*controlURL) == "" {
		*controlURL = env("BRUISER_CHECK_CONTROL_URL", "http://127.0.0.1:8080")
	}
	if strings.TrimSpace(*adminURL) == "" {
		*adminURL = env("BRUISER_CHECK_ADMIN_URL", "http://127.0.0.1:8082")
	}
	rep, err := check.Run(check.Config{
		EdgeURL:    front,
		OriginURL:  *originURL,
		ControlURL: *controlURL,
		AdminURL:   *adminURL,
		HMACSecret: *secret,
		Membership: *membership,
		EventID:    *eventID,
	})
	if err != nil {
		return err
	}
	fmt.Print(rep.String())
	if err := persistAuthorityCheck(rep); err != nil {
		fmt.Fprintf(os.Stderr, "authority-check audit not persisted: %v\n", err)
	}
	if !rep.Passed() {
		return fmt.Errorf("authority check FAIL")
	}
	return nil
}

func persistAuthorityCheck(rep check.Report) error {
	cfg := config.Load()
	if cfg.DatabaseURL == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	store, err := pgstore.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer store.Close()
	reason := rep.Overall
	attrs := map[string]any{
		"overall": rep.Overall,
		"probes":  rep.Probes,
		"covered": rep.Covered,
	}
	return store.WriteAudit(ctx, cfg.MerchantID, "AUTHORITY_CHECK", reason, "", attrs)
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
