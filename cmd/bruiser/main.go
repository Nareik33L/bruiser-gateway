package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/api/public"
	"github.com/Nareik33L/bruiser-gateway/internal/attacklab"
	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/config"
	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
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
	fmt.Fprintf(os.Stderr, "usage: bruiser <serve|migrate|assertion|authority-check>\n")
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
	go sweep(store, cfg.SweepInterval, log, sweeperStop)

	profile := withAttackLabRoutes(loadProfile(cfg, log))
	gw := publicapi.New(cfg, store, signer, log, profile)
	lab, err := attacklab.New(attacklab.Config{
		HMACSecret:   cfg.DevHMACSecret,
		EdgeSecret:   cfg.EdgeSecret,
		OriginSecret: cfg.OriginSecret,
		Gateway:      gw,
		Log:          log,
	})
	if err != nil {
		return fmt.Errorf("attack lab: %w", err)
	}
	go sweepLab(lab, 30*time.Second, sweeperStop)

	handler := composeSite(gw, lab)
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      0,
		IdleTimeout:       120 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.HTTPAddr, "merchant", cfg.MerchantID, "site", "/")
		errCh <- srv.ListenAndServe()
	}()

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
	return srv.Shutdown(shutCtx)
}

func sweep(store *pgstore.Store, every time.Duration, log *slog.Logger, stop <-chan struct{}) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			n, err := store.ExpireDue(ctx, 200)
			cancel()
			if err != nil {
				log.Warn("sweep failed", "err", err)
				continue
			}
			if n > 0 {
				log.Info("expired executions", "count", n)
			}
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
