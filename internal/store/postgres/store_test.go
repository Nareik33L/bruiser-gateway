package postgres_test

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/id"
	"github.com/Nareik33L/bruiser-gateway/internal/lease"
	pgstore "github.com/Nareik33L/bruiser-gateway/internal/store/postgres"
)

func testURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("BRUISER_TEST_DATABASE_URL")
	if u == "" {
		t.Skip("BRUISER_TEST_DATABASE_URL not set")
	}
	return u
}

var migrateOnce sync.Once

func connect(t *testing.T) *pgstore.Store {
	t.Helper()
	ctx := context.Background()
	url := testURL(t)
	var migrateErr error
	migrateOnce.Do(func() {
		migrateErr = pgstore.Migrate(ctx, url)
	})
	if migrateErr != nil {
		t.Fatalf("migrate: %v", migrateErr)
	}
	s, err := pgstore.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func acquireReq(merchant, customer, agent, resource string) lease.AcquireRequest {
	return lease.AcquireRequest{
		MerchantID:  merchant,
		DomainKey:   lease.DomainKey(merchant, lease.DefaultRuleName(), customer, resource),
		CustomerID:  customer,
		Principal:   lease.Principal{Type: "agent", ID: agent},
		SessionID:   id.Session(),
		Resource:    resource,
		Action:      "purchase",
		RuleName:    lease.DefaultRuleName(),
		MaxActive:   1,
		TTL:         5 * time.Second,
		MaxLifetime: time.Minute,
		RequestID:   id.Request(),
	}
}

func TestAcquireExclusiveAndIdempotent(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	if err := s.EnsureMerchant(ctx, m, "test", "secret"); err != nil {
		t.Fatal(err)
	}
	req := acquireReq(m, "alice", "agent-1", "event:ars-che")
	r1, err := s.Acquire(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Status != lease.StatusGranted {
		t.Fatalf("status=%s", r1.Status)
	}
	r2, err := s.Acquire(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Status != lease.StatusAlreadyHeld {
		t.Fatalf("status=%s want ALREADY_HELD", r2.Status)
	}
	if r1.Execution.ID != r2.Execution.ID {
		t.Fatalf("idempotent acquire returned different executions")
	}

	other := acquireReq(m, "alice", "agent-2", "event:ars-che")
	r3, err := s.Acquire(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	if r3.Status != lease.StatusBusy {
		t.Fatalf("status=%s want BUSY", r3.Status)
	}
	if r3.Busy.ActiveExecutionID != r1.Execution.ID {
		t.Fatalf("busy holder %s want %s", r3.Busy.ActiveExecutionID, r1.Execution.ID)
	}

	n, err := s.CountAudit(ctx, m, "EXECUTION_GRANTED")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("granted audits = %d want 1", n)
	}
}

func TestReleaseThenReacquire(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	_ = s.EnsureMerchant(ctx, m, "test", "secret")
	req := acquireReq(m, "alice", "agent-1", "event:ars-che")
	r1, err := s.Acquire(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Release(ctx, m, r1.Execution.ID, req.SessionID, id.Request()); err != nil {
		t.Fatal(err)
	}
	r2, err := s.Acquire(ctx, acquireReq(m, "alice", "agent-2", "event:ars-che"))
	if err != nil {
		t.Fatal(err)
	}
	if r2.Status != lease.StatusGranted {
		t.Fatalf("status=%s", r2.Status)
	}
	if r2.Execution.Fence <= r1.Execution.Fence {
		t.Fatalf("fence did not increase: %d then %d", r1.Execution.Fence, r2.Execution.Fence)
	}
}

func TestRenewAndMaxLifetime(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	_ = s.EnsureMerchant(ctx, m, "test", "secret")
	req := acquireReq(m, "alice", "agent-1", "event:ars-che")
	req.TTL = 2 * time.Second
	req.MaxLifetime = 3 * time.Second
	r1, err := s.Acquire(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.Renew(ctx, m, r1.Execution.ID, req.SessionID, id.Request(), 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !e.ExpiresAt.Before(e.MaxLifetimeAt.Add(time.Millisecond)) && !e.ExpiresAt.Equal(e.MaxLifetimeAt) {
		if e.ExpiresAt.After(e.MaxLifetimeAt) {
			t.Fatalf("renew extended past max lifetime: exp=%s max=%s", e.ExpiresAt, e.MaxLifetimeAt)
		}
	}
}

func TestExpireDueMaterialises(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	_ = s.EnsureMerchant(ctx, m, "test", "secret")
	req := acquireReq(m, "alice", "agent-1", "event:ars-che")
	req.TTL = 50 * time.Millisecond
	r1, err := s.Acquire(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	n, err := s.ExpireDue(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("expected at least one expiry, got %d", n)
	}
	got, err := s.Get(ctx, m, r1.Execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != lease.StateExpired {
		t.Fatalf("state=%s want EXPIRED", got.State)
	}
	r2, err := s.Acquire(ctx, acquireReq(m, "alice", "agent-2", "event:ars-che"))
	if err != nil {
		t.Fatal(err)
	}
	if r2.Status != lease.StatusGranted {
		t.Fatalf("status=%s after expiry", r2.Status)
	}
}

func TestConcurrentAcquireExactlyOne(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	_ = s.EnsureMerchant(ctx, m, "test", "secret")
	const n = 200
	var wg sync.WaitGroup
	wg.Add(n)
	results := make([]lease.AcquireResult, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			req := acquireReq(m, "alice", id.New("ag"), "event:final")
			results[i], errs[i] = s.Acquire(ctx, req)
		}()
	}
	wg.Wait()
	granted := 0
	busy := 0
	var grantedID string
	for i, r := range results {
		if errs[i] != nil {
			t.Fatalf("acquire %d: %v", i, errs[i])
		}
		switch r.Status {
		case lease.StatusGranted:
			granted++
			grantedID = r.Execution.ID
		case lease.StatusBusy:
			busy++
		default:
			t.Fatalf("unexpected status %s", r.Status)
		}
	}
	if granted != 1 {
		t.Fatalf("granted=%d busy=%d want granted=1", granted, busy)
	}
	if granted+busy != n {
		t.Fatalf("granted+busy=%d want %d", granted+busy, n)
	}
	_ = grantedID
}
