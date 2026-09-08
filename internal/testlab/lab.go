package testlab

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/api/public"
	"github.com/Nareik33L/bruiser-gateway/internal/auth"
	"github.com/Nareik33L/bruiser-gateway/internal/config"
	"github.com/Nareik33L/bruiser-gateway/internal/id"
	"github.com/Nareik33L/bruiser-gateway/internal/merchant"
	pgstore "github.com/Nareik33L/bruiser-gateway/internal/store/postgres"
)

func ArsenalProfile(t testing.TB) merchant.Profile {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		p := filepath.Join(dir, "configs", "arsenal.yaml")
		if _, err := os.Stat(p); err == nil {
			prof, err := merchant.LoadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			return prof
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("configs/arsenal.yaml not found")
	return merchant.Profile{}
}

func Gateway(t testing.TB, profile merchant.Profile) (*httptest.Server, config.Config) {
	t.Helper()
	_, srv, cfg, _ := GatewayAPI(t, profile)
	return srv, cfg
}

func GatewayAPI(t testing.TB, profile merchant.Profile) (*publicapi.Server, *httptest.Server, config.Config, auth.Signer) {
	t.Helper()
	url := os.Getenv("BRUISER_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("BRUISER_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	if err := pgstore.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	store, err := pgstore.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	cfg := config.Load()
	cfg.DatabaseURL = url
	cfg.MerchantID = id.New("m")
	cfg.LeaseTTL = 30 * time.Second
	cfg.EdgeSecret = "edge-secret-dev"
	cfg.OriginSecret = "origin-lock-dev"
	cfg.MaxInFlight = 8
	cfg.RatePerSec = 100
	if profile.MerchantID == "" {
		profile = merchant.Empty(cfg.MerchantID)
	}
	if err := store.EnsureMerchant(ctx, cfg.MerchantID, "test", cfg.DevHMACSecret); err != nil {
		t.Fatal(err)
	}
	key, err := store.EnsureSigningKey(ctx, cfg.MerchantID)
	if err != nil {
		t.Fatal(err)
	}
	signer := auth.Signer{KID: key.KID, MerchantID: cfg.MerchantID, Private: key.Private, Public: key.Public}
	api := publicapi.New(cfg, store, signer, slog.New(slog.NewTextHandler(io.Discard, nil)), profile)
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	return api, srv, cfg, signer
}
