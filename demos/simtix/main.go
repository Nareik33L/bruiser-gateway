package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/Nareik33L/bruiser-gateway/demos/seed"
	"github.com/Nareik33L/bruiser-gateway/demos/shared"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	originAddr := shared.Env("SIMTIX_HTTP_ADDR", ":8090")
	edgeAddr := shared.Env("SIMTIX_EDGE_ADDR", ":8091")
	originSecret := shared.Env("SIMTIX_ORIGIN_SECRET", "origin-lock-dev")
	hmac := shared.Env("BRUISER_DEV_HMAC_SECRET", "dev-secret-change-me")
	sso := shared.Env("HARCHESTER_SSO_SECRET", "harchester-sso-dev")
	bruiser := shared.Env("SIMTIX_BRUISER_URL", "http://127.0.0.1:8080")
	edgeSecret := shared.Env("BRUISER_EDGE_SECRET", "edge-secret-dev")
	adminSecret := shared.Env("DEMO_ADMIN_SECRET", "demo-admin-dev")
	opponent := shared.Env("DEMO_OPPONENT", seed.OpponentDefault)
	publicURL := shared.Env("SIMTIX_PUBLIC_URL", "http://127.0.0.1:8091")

	origin := newOrigin(originConfig{
		HMACSecret:   hmac,
		SSOSecret:    sso,
		OriginSecret: originSecret,
		AdminSecret:  adminSecret,
		Opponent:     opponent,
		PublicURL:    publicURL,
	})
	originSrv := &http.Server{
		Addr:              originAddr,
		Handler:           origin.handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Info("simtix origin listening", "addr", originAddr, "lockdown", originSecret != "")
		if err := originSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("origin", "err", err)
			os.Exit(1)
		}
	}()

	edge := newEdge(edgeConfig{
		OriginHandler: origin.handler(),
		BruiserURL:    bruiser,
		EdgeSecret:    edgeSecret,
		OriginSecret:  originSecret,
		AdminSecret:   adminSecret,
		HMACSecret:    hmac,
	})
	edgeSrv := &http.Server{
		Addr:              edgeAddr,
		Handler:           edge.handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	log.Info("simtix edge listening", "addr", edgeAddr, "bruiser", bruiser)
	if err := edgeSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Error("edge", "err", err)
		os.Exit(1)
	}
}
