package main

import (
	"crypto/ed25519"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/edge"
	"github.com/Nareik33L/bruiser-gateway/internal/simtix"
	bruiser "github.com/Nareik33L/bruiser-gateway/sdk/go"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	originAddr := env("SIMTIX_HTTP_ADDR", ":8090")
	edgeAddr := os.Getenv("SIMTIX_EDGE_ADDR")
	originSecret := os.Getenv("SIMTIX_ORIGIN_SECRET")
	hmac := env("BRUISER_DEV_HMAC_SECRET", "dev-secret-change-me")
	bruiserURL := env("SIMTIX_BRUISER_URL", "http://127.0.0.1:8080")
	merchant := env("BRUISER_MERCHANT_ID", "arsenal")

	pub := fetchExecutionPublic(log, bruiserURL)
	origin := simtix.New(simtix.Lab(hmac, originSecret, merchant, pub, bruiserURL, 0))
	originSrv := &http.Server{
		Addr:              originAddr,
		Handler:           origin.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		log.Info("simtix origin listening", "addr", originAddr, "lockdown", originSecret != "", "jwks", pub != nil)
		if err := originSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("origin", "err", err)
			os.Exit(1)
		}
	}()

	if edgeAddr != "" {
		p, err := edge.New(edge.Config{
			OriginURL:    env("SIMTIX_ORIGIN_URL", "http://127.0.0.1"+originAddr),
			BruiserURL:   bruiserURL,
			EdgeSecret:   env("BRUISER_EDGE_SECRET", "edge-secret-dev"),
			OriginSecret: env("BRUISER_ORIGIN_SECRET", "origin-lock-dev"),
			MaxInFlight:  2,
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

func fetchExecutionPublic(log *slog.Logger, bruiserURL string) ed25519.PublicKey {
	url := strings.TrimRight(bruiserURL, "/") + "/.well-known/bruiser/jwks.json"
	client := &http.Client{Timeout: 2 * time.Second}
	var last error
	for i := 0; i < 30; i++ {
		resp, err := client.Get(url)
		if err != nil {
			last = err
			time.Sleep(500 * time.Millisecond)
			continue
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			last = err
			time.Sleep(500 * time.Millisecond)
			continue
		}
		pub, err := bruiser.PublicFromJWKS(b)
		if err != nil {
			last = err
			time.Sleep(500 * time.Millisecond)
			continue
		}
		return pub
	}
	log.Error("jwks fetch failed; origin will reject allocation", "url", url, "err", last)
	return nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
