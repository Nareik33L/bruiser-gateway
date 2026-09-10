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
	cfg.ProfilePath = *profile
	cfg.OverlayProfile(p.Identity.JWKSURL, p.Identity.Issuer, p.Identity.Audience, p.MerchantID, p.Name)
	httpURL := *httpBase
	if httpURL == "" && *front != "" {
		httpURL = env("BRUISER_CHECK_CONTROL_URL", "http://127.0.0.1:8080")
	}
	rep := doctor.Run(doctor.Input{
		Config:     cfg,
		Profile:    p,
		ProbeStore: !*skipStore,
		HTTPBase:   httpURL,
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
	cfg.ProfilePath = path
	fails := 0
	p, err := merchant.LoadFile(path)
	if err != nil {
		fmt.Printf("FAIL  profile: %s\n", err)
		return err
	}
	cfg.OverlayProfile(p.Identity.JWKSURL, p.Identity.Issuer, p.Identity.Audience, p.MerchantID, p.Name)
	if err := cfg.Validate(); err != nil {
		fmt.Printf("FAIL  environment: %s\n", err)
		fails++
	} else {
		fmt.Printf("PASS  environment: mode=%s lease_ttl=%s heartbeat=%s\n", cfg.Mode, cfg.LeaseTTL, cfg.HeartbeatInterval)
		fmt.Printf("PASS  posture: %s\n", cfg.StartupBanner())
		for _, w := range cfg.SecretWarnings() {
			fmt.Printf("WARN  secrets: %s\n", w)
		}
		for _, w := range cfg.UnsafeModeWarnings() {
			fmt.Printf("WARN  unsafe: %s\n", w)
		}
		for _, w := range cfg.ProductionWarnings() {
			fmt.Printf("WARN  production: %s\n", w)
		}
	}
	issues := append(p.ValidateIssues(), p.SafetyIssues(merchant.SafetyOpts{
		OriginSecret: cfg.OriginSecret,
		OriginURL:    cfg.OriginURL,
		Proxy:        cfg.ProxyAddr != "",
		Production:   cfg.Production(),
		JWKSURL:      cfg.JWKSURL,
		Issuer:       cfg.Issuer,
		Audience:     cfg.Audience,
	})...)
	if cfg.Production() {
		if err := merchant.ValidateProductionPolicy(p, cfg.AllowUnsafeModes); err != nil {
			issues = append(issues, merchant.Issue{Level: "FAIL", Field: "policy", Message: err.Error()})
		}
	}
	for _, i := range issues {
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
