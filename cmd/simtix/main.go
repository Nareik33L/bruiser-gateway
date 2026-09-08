package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/edge"
	"github.com/Nareik33L/bruiser-gateway/internal/simtix"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	originAddr := env("SIMTIX_HTTP_ADDR", ":8090")
	edgeAddr := os.Getenv("SIMTIX_EDGE_ADDR")
	originSecret := os.Getenv("SIMTIX_ORIGIN_SECRET")
	hmac := env("BRUISER_DEV_HMAC_SECRET", "dev-secret-change-me")

	origin := simtix.New(simtix.Config{
		HMACSecret:   hmac,
		OriginSecret: originSecret,
	})
	originSrv := &http.Server{
		Addr:              originAddr,
		Handler:           origin.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Info("simtix origin listening", "addr", originAddr, "lockdown", originSecret != "")
		if err := originSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("origin", "err", err)
			os.Exit(1)
		}
	}()

	if edgeAddr != "" {
		p, err := edge.New(edge.Config{
			OriginURL:   env("SIMTIX_ORIGIN_URL", "http://127.0.0.1"+originAddr),
			BruiserURL:  env("SIMTIX_BRUISER_URL", "http://127.0.0.1:8080"),
			EdgeSecret:  env("BRUISER_EDGE_SECRET", "edge-secret-dev"),
			MaxInFlight: 2,
		})
		if err != nil {
			log.Error("edge config", "err", err)
			os.Exit(1)
		}
		edgeSrv := &http.Server{
			Addr:              edgeAddr,
			Handler:           p.Handler(),
			ReadHeaderTimeout: 5 * time.Second,
		}
		log.Info("simtix edge listening", "addr", edgeAddr)
		if err := edgeSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("edge", "err", err)
			os.Exit(1)
		}
		return
	}

	select {}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
