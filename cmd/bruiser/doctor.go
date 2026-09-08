package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Nareik33L/bruiser-gateway/internal/config"
	"github.com/Nareik33L/bruiser-gateway/internal/doctor"
	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
)

func cmdDoctor() error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	profile := fs.String("profile", env("BRUISER_PROFILE", "configs/example.yaml"), "merchant profile YAML")
	httpBase := fs.String("http", "", "control-plane base URL to probe /metrics and /readyz")
	front := fs.String("front", "", "Edge/Proxy URL — run authority-check when set")
	origin := fs.String("origin", "", "upstream origin URL")
	skipStore := fs.Bool("skip-store", false, "do not ping Postgres or signing keys")
	if err := fs.Parse(os.Args[2:]); err != nil {
		return err
	}
	cfg := config.Load()
	if *origin != "" {
		cfg.OriginURL = *origin
	}
	p, err := merchant.LoadFile(*profile)
	if err != nil {
		return fmt.Errorf("profile: %w", err)
	}
	if p.MerchantID != "" && os.Getenv("BRUISER_MERCHANT_ID") == "" {
		cfg.MerchantID = p.MerchantID
	}
	rep := doctor.Run(doctor.Input{
		Config:     cfg,
		Profile:    p,
		ProbeStore: !*skipStore,
		HTTPBase:   *httpBase,
		FrontURL:   *front,
		OriginURL:  *origin,
	})
	fmt.Print(rep.String())
	if !rep.Passed() {
		return fmt.Errorf("doctor FAIL")
	}
	return nil
}

func cmdConfig() error {
	if len(os.Args) < 3 || os.Args[2] != "validate" {
		return fmt.Errorf("usage: bruiser config validate [file.yaml]")
	}
	cfg := config.Load()
	path := cfg.ProfilePath
	if len(os.Args) > 3 {
		path = os.Args[3]
	}
	fails := 0
	if err := cfg.Validate(); err != nil {
		fmt.Printf("FAIL  environment: %s\n", err)
		fails++
	} else {
		fmt.Printf("PASS  environment: mode=%s lease_ttl=%s heartbeat=%s\n", cfg.Mode, cfg.LeaseTTL, cfg.HeartbeatInterval)
	}
	p, err := merchant.LoadFile(path)
	if err != nil {
		fmt.Printf("FAIL  profile: %s\n", err)
		return err
	}
	for _, i := range p.ValidateIssues() {
		fmt.Printf("%s  %s: %s\n", i.Level, i.Field, i.Message)
		if i.Level == "FAIL" {
			fails++
		}
	}
	if fails > 0 {
		return fmt.Errorf("config validate: %d error(s)", fails)
	}
	fmt.Printf("ok merchant=%s file=%s extractor=%s unmatched=%s\n", p.MerchantID, path, p.Identity.Extractor, p.Unmatched)
	return nil
}
