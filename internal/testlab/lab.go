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
	lab := Start(t, profile, nil)
	return lab.Server, lab.Cfg
}

func GatewayAPI(t testing.TB, profile merchant.Profile) (*publicapi.Server, *httptest.Server, config.Config, auth.Signer) {
	lab := Start(t, profile, nil)
	return lab.API, lab.Server, lab.Cfg, lab.Signer
}

type Lab struct {
	API    *publicapi.Server
	Server *httptest.Server
	Admin  *httptest.Server
	Cfg    config.Config
	Signer auth.Signer
	Store  *pgstore.Store
}

func Start(t testing.TB, profile merchant.Profile, mut func(*config.Config)) Lab {
	t.Helper()
	return gatewayWithStore(t, profile, mut)
}

func GatewayWith(t testing.TB, profile merchant.Profile, mut func(*config.Config)) (*publicapi.Server, *httptest.Server, config.Config, auth.Signer) {
	lab := Start(t, profile, mut)
	return lab.API, lab.Server, lab.Cfg, lab.Signer
}

func gatewayWithStore(t testing.TB, profile merchant.Profile, mut func(*config.Config)) Lab {
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
	cfg.Environment = "lab"
	cfg.NormalizeListen()
	cfg.DatabaseURL = url
	cfg.MerchantID = id.New("m")
	cfg.LeaseTTL = 30 * time.Second
	cfg.EdgeSecret = "edge-secret-dev"
	cfg.AdminSecret = "admin-secret-dev"
	cfg.OperatorSecret = "operator-secret-dev"
	cfg.OriginSecret = "origin-lock-dev"
	cfg.AdminAddr = "127.0.0.1:0"
	cfg.MaxInFlight = 8
	cfg.RatePerSec = 100
	cfg.DevAssertions = true
	cfg.RateSessions = 100000
	cfg.RateAcquire = 100000
	cfg.RateRenew = 100000
	cfg.RateRelease = 100000
	cfg.RateAuthorize = 100000
	cfg.RateMerchant = 100000
	cfg.RateCustomer = 100000
	cfg.RatePrincipal = 100000
	cfg.RateIP = 0
	if mut != nil {
		mut(&cfg)
	}
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
	pub := httptest.NewServer(api)
	admin := httptest.NewServer(api.AdminHandler())
	t.Cleanup(func() {
		api.Close()
		pub.Close()
		admin.Close()
	})
	return Lab{API: api, Server: pub, Admin: admin, Cfg: cfg, Signer: signer, Store: store}
}

// GatewayPair starts two control-plane processes against one merchant so
// LISTEN/NOTIFY and cache-reset behaviour can be tested across nodes.
func GatewayPair(t testing.TB, profile merchant.Profile) (Lab, Lab) {
	t.Helper()
	a := Start(t, profile, nil)
	url := a.Cfg.DatabaseURL
	ctx := context.Background()
	storeB, err := pgstore.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(storeB.Close)
	apiB := publicapi.New(a.Cfg, storeB, a.Signer, slog.New(slog.NewTextHandler(io.Discard, nil)), profile)
	pubB := httptest.NewServer(apiB)
	adminB := httptest.NewServer(apiB.AdminHandler())
	t.Cleanup(func() {
		apiB.Close()
		pubB.Close()
		adminB.Close()
	})
	a.API.Start(ctx)
	apiB.Start(ctx)
	return a, Lab{API: apiB, Server: pubB, Admin: adminB, Cfg: a.Cfg, Signer: a.Signer, Store: storeB}
}
