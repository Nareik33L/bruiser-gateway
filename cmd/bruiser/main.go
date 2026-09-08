package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/api/public"
	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/config"
	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
	"github.com/Nareik33L/bruiser-gateway/internal/policy"
	pgstore "github.com/Nareik33L/bruiser-gateway/internal/store/postgres"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cfg := config.Load()
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parseLevel(cfg.LogLevel)}))
	var err error
	switch os.Args[1] {
	case "serve":
		err = cmdServe(cfg, log)
	case "migrate":
		err = cmdMigrate(cfg, log)
	case "assertion":
		err = cmdAssertion(cfg)
	case "authority-check":
		err = cmdAuthorityCheck()
	case "eaf-demo":
		err = cmdEAFDemo()
	case "swarm":
		err = cmdSwarm()
	case "policy":
		err = cmdPolicy()
	case "profile":
		err = cmdProfile()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		log.Error("command failed", "err", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: bruiser <serve|migrate|assertion|authority-check|eaf-demo|swarm|policy validate|profile validate|profile init>\n")
}

func cmdMigrate(cfg config.Config, log *slog.Logger) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := pgstore.Migrate(ctx, cfg.DatabaseURL); err != nil {
		return err
	}
	log.Info("migrations applied")
	return nil
}

func cmdServe(cfg config.Config, log *slog.Logger) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	ctx := context.Background()
	store, err := pgstore.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(ctx); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	if err := store.EnsureMerchant(ctx, cfg.MerchantID, cfg.MerchantName, cfg.DevHMACSecret); err != nil {
		return fmt.Errorf("ensure merchant: %w", err)
	}
	key, err := store.EnsureSigningKey(ctx, cfg.MerchantID)
	if err != nil {
		return fmt.Errorf("ensure signing key: %w", err)
	}
	signer := auth.Signer{
		KID:        key.KID,
		MerchantID: cfg.MerchantID,
		Private:    key.Private,
		Public:     key.Public,
	}

	sweeperStop := make(chan struct{})
	go sweep(store, cfg, log, sweeperStop)

	if cfg.ProxyAddr != "" && cfg.OriginURL == "" {
		return fmt.Errorf("BRUISER_ORIGIN_URL is required when BRUISER_PROXY_ADDR is set")
	}

	if !cfg.Telemetry {
		log.Info("telemetry disabled (merchant-controlled; no vendor phone-home)")
	}
	handler := publicapi.New(cfg, store, signer, log, loadProfile(cfg, log))
	handler.Start(ctx)
	defer handler.Close()
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      35 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.HTTPAddr, "merchant", cfg.MerchantID)
		errCh <- srv.ListenAndServe()
	}()

	var proxySrv *http.Server
	if cfg.ProxyAddr != "" {
		ph, err := handler.ProxyHandler(cfg.OriginURL)
		if err != nil {
			return err
		}
		proxySrv = &http.Server{
			Addr:              cfg.ProxyAddr,
			Handler:           ph,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      35 * time.Second,
			IdleTimeout:       60 * time.Second,
		}
		go func() {
			log.Info("proxy listening", "addr", cfg.ProxyAddr, "origin", cfg.OriginURL)
			errCh <- proxySrv.ListenAndServe()
		}()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-sigCh:
		log.Info("shutdown signal", "signal", sig.String())
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
	}

	close(sweeperStop)
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if proxySrv != nil {
		_ = proxySrv.Shutdown(shutCtx)
	}
	return srv.Shutdown(shutCtx)
}

func sweep(store *pgstore.Store, cfg config.Config, log *slog.Logger, stop <-chan struct{}) {
	t := time.NewTicker(cfg.SweepInterval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			n, err := store.ExpireDue(ctx, 200)
			if err != nil {
				log.Warn("sweep failed", "err", err)
			} else if n > 0 {
				log.Info("expired executions", "count", n)
			}
			if cfg.AuditRetention > 0 {
				purged, perr := store.PurgeAudit(ctx, cfg.MerchantID, time.Now().UTC().Add(-cfg.AuditRetention))
				if perr != nil {
					log.Warn("audit purge failed", "err", perr)
				} else if purged > 0 {
					log.Info("purged audit events", "count", purged)
				}
			}
			cancel()
		}
	}
}

func cmdAssertion(cfg config.Config) error {
	customer := "1001234"
	if len(os.Args) > 2 {
		customer = os.Args[2]
	}
	tok, err := auth.IssueBoxOfficeSession(cfg.DevHMACSecret, customer, time.Hour)
	if err != nil {
		return err
	}
	fmt.Println(tok)
	return nil
}

func cmdPolicy() error {
	if len(os.Args) < 3 || os.Args[2] != "validate" {
		return fmt.Errorf("usage: bruiser policy validate [file.yaml]")
	}
	var raw []byte
	var err error
	if len(os.Args) > 3 {
		raw, err = os.ReadFile(os.Args[3])
	} else {
		raw, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		return err
	}
	c, err := policy.CompileYAML(raw)
	if err != nil {
		return err
	}
	fmt.Printf("ok version=%d rules=%d fallback=%s\n", c.Doc.Version, len(c.Doc.Domains), c.Doc.Fallback)
	return nil
}

func loadProfile(cfg config.Config, log *slog.Logger) merchant.Profile {
	p, err := merchant.LoadFile(cfg.ProfilePath)
	if err != nil {
		log.Warn("profile file missing; using empty profile", "path", cfg.ProfilePath, "err", err)
		return merchant.Empty(cfg.MerchantID)
	}
	if p.MerchantID == "" {
		p.MerchantID = cfg.MerchantID
	}
	log.Info("loaded merchant profile", "merchant", p.MerchantID, "routes", len(p.Routes))
	return p
}

func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
