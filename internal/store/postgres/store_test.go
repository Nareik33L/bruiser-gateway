package postgres_test

import (
	"context"
	"errors"
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

func TestResumeBeforeExpirySameExecution(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	if err := s.EnsureMerchant(ctx, m, "test", "secret"); err != nil {
		t.Fatal(err)
	}
	req := acquireReq(m, "alice", "agent-1", "event:ars-che")
	req.TTL = 2 * time.Second
	r1, err := s.Acquire(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Status != lease.StatusGranted {
		t.Fatalf("status=%s", r1.Status)
	}
	time.Sleep(200 * time.Millisecond)

	resume := req
	resume.SessionID = id.Session()
	resume.RequestID = id.Request()
	r2, err := s.Acquire(ctx, resume)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Status != lease.StatusAlreadyHeld {
		t.Fatalf("status=%s want ALREADY_HELD", r2.Status)
	}
	if r2.Execution.ID != r1.Execution.ID {
		t.Fatalf("resume returned %s want %s", r2.Execution.ID, r1.Execution.ID)
	}
	if !r2.Execution.ExpiresAt.After(r1.Execution.ExpiresAt) {
		t.Fatalf("resume did not extend ttl: first=%s resume=%s", r1.Execution.ExpiresAt, r2.Execution.ExpiresAt)
	}
	if r2.Execution.RenewCount != r1.Execution.RenewCount+1 {
		t.Fatalf("renew_count=%d want %d", r2.Execution.RenewCount, r1.Execution.RenewCount+1)
	}
	if r2.Execution.SessionID != resume.SessionID {
		t.Fatalf("session not rebound: %s want %s", r2.Execution.SessionID, resume.SessionID)
	}
	n, err := s.CountAudit(ctx, m, "EXECUTION_RENEWED")
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("expected EXECUTION_RENEWED audit, got %d", n)
	}
}

func TestAcquireAfterExpiryGrantsNew(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	if err := s.EnsureMerchant(ctx, m, "test", "secret"); err != nil {
		t.Fatal(err)
	}
	req := acquireReq(m, "alice", "agent-1", "event:ars-che")
	req.TTL = 80 * time.Millisecond
	r1, err := s.Acquire(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	r2, err := s.Acquire(ctx, acquireReq(m, "alice", "agent-1", "event:ars-che"))
	if err != nil {
		t.Fatal(err)
	}
	if r2.Status != lease.StatusGranted {
		t.Fatalf("status=%s want GRANTED after expiry", r2.Status)
	}
	if r2.Execution.ID == r1.Execution.ID {
		t.Fatalf("expected a new execution after expiry, got %s", r2.Execution.ID)
	}
}

func TestRenewFromNewSessionSamePrincipal(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	if err := s.EnsureMerchant(ctx, m, "test", "secret"); err != nil {
		t.Fatal(err)
	}
	req := acquireReq(m, "alice", "agent-1", "event:ars-che")
	if err := s.InsertSession(ctx, pgstore.SessionRow{
		ID: req.SessionID, MerchantID: m, CustomerID: "alice",
		PrincipalType: "agent", PrincipalID: "agent-1",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	r1, err := s.Acquire(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	newSess := id.Session()
	if err := s.InsertSession(ctx, pgstore.SessionRow{
		ID: newSess, MerchantID: m, CustomerID: "alice",
		PrincipalType: "agent", PrincipalID: "agent-1",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	e, err := s.Renew(ctx, m, r1.Execution.ID, newSess, id.Request(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if e.SessionID != newSess {
		t.Fatalf("heartbeat did not rebind session: %s want %s", e.SessionID, newSess)
	}
	if e.RenewCount < 1 {
		t.Fatalf("renew_count=%d", e.RenewCount)
	}

	other := id.Session()
	if err := s.InsertSession(ctx, pgstore.SessionRow{
		ID: other, MerchantID: m, CustomerID: "alice",
		PrincipalType: "agent", PrincipalID: "agent-2",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Renew(ctx, m, r1.Execution.ID, other, id.Request(), 5*time.Second); !errors.Is(err, lease.ErrNotHolder) {
		t.Fatalf("foreign principal renew err=%v want ErrNotHolder", err)
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

func insertSess(t *testing.T, s *pgstore.Store, merchant, sessID, customer, ptype, pid string) {
	t.Helper()
	if err := s.InsertSession(context.Background(), pgstore.SessionRow{
		ID: sessID, MerchantID: merchant, CustomerID: customer,
		PrincipalType: ptype, PrincipalID: pid,
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPreemptiveHandoffAndRenewGone(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	if err := s.EnsureMerchant(ctx, m, "test", "secret"); err != nil {
		t.Fatal(err)
	}
	agent := acquireReq(m, "alice", "agent-1", "event:ars-che")
	insertSess(t, s, m, agent.SessionID, "alice", "agent", "agent-1")
	r1, err := s.Acquire(ctx, agent)
	if err != nil {
		t.Fatal(err)
	}

	browserSess := id.Session()
	insertSess(t, s, m, browserSess, "alice", "browser", "alice-tab")
	res, err := s.Handoff(ctx, lease.HandoffRequest{
		MerchantID:  m,
		ExecutionID: r1.Execution.ID,
		SessionID:   browserSess,
		Mode:        lease.ModePreempt,
		TTL:         5 * time.Second,
		RequestID:   id.Request(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Mode != lease.ModePreempt {
		t.Fatalf("mode=%s", res.Mode)
	}
	if res.Successor.Fence <= r1.Execution.Fence {
		t.Fatalf("fence did not increase: %d → %d", r1.Execution.Fence, res.Successor.Fence)
	}
	if res.Successor.Principal.Type != "browser" {
		t.Fatalf("successor principal %v", res.Successor.Principal)
	}

	_, err = s.Renew(ctx, m, r1.Execution.ID, agent.SessionID, id.Request(), 5*time.Second)
	var gone *lease.GoneError
	if !errors.As(err, &gone) || gone.Reason != lease.StateHandedOff {
		t.Fatalf("renew after handoff err=%v", err)
	}
	if gone.SuccessorID != res.Successor.ID {
		t.Fatalf("successor_id=%s want %s", gone.SuccessorID, res.Successor.ID)
	}

	got, err := s.Get(ctx, m, r1.Execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != lease.StateHandedOff {
		t.Fatalf("state=%s", got.State)
	}
}

func TestCooperativeHandoffRequiresHolder(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	_ = s.EnsureMerchant(ctx, m, "test", "secret")
	agent := acquireReq(m, "alice", "agent-1", "event:ars-che")
	insertSess(t, s, m, agent.SessionID, "alice", "agent", "agent-1")
	r1, err := s.Acquire(ctx, agent)
	if err != nil {
		t.Fatal(err)
	}

	other := id.Session()
	insertSess(t, s, m, other, "alice", "agent", "agent-2")
	_, err = s.Handoff(ctx, lease.HandoffRequest{
		MerchantID:  m,
		ExecutionID: r1.Execution.ID,
		SessionID:   other,
		To:          lease.Principal{Type: "browser", ID: "tab"},
		Mode:        lease.ModeCooperative,
		TTL:         time.Second,
		RequestID:   id.Request(),
	})
	if !errors.Is(err, lease.ErrNotHolder) {
		t.Fatalf("err=%v want ErrNotHolder", err)
	}

	res, err := s.Handoff(ctx, lease.HandoffRequest{
		MerchantID:  m,
		ExecutionID: r1.Execution.ID,
		SessionID:   agent.SessionID,
		To:          lease.Principal{Type: "browser", ID: "tab"},
		Mode:        lease.ModeCooperative,
		TTL:         time.Second,
		RequestID:   id.Request(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Successor.Principal.ID != "tab" {
		t.Fatalf("to=%v", res.Successor.Principal)
	}
}

func TestAgentCannotPreemptBrowser(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	_ = s.EnsureMerchant(ctx, m, "test", "secret")
	browser := acquireReq(m, "alice", "tab", "event:ars-che")
	browser.Principal = lease.Principal{Type: "browser", ID: "tab"}
	insertSess(t, s, m, browser.SessionID, "alice", "browser", "tab")
	r1, err := s.Acquire(ctx, browser)
	if err != nil {
		t.Fatal(err)
	}
	agentSess := id.Session()
	insertSess(t, s, m, agentSess, "alice", "agent", "bot")
	_, err = s.Handoff(ctx, lease.HandoffRequest{
		MerchantID:  m,
		ExecutionID: r1.Execution.ID,
		SessionID:   agentSess,
		Mode:        lease.ModePreempt,
		TTL:         time.Second,
		RequestID:   id.Request(),
	})
	if !errors.Is(err, lease.ErrPrecedence) {
		t.Fatalf("err=%v want ErrPrecedence", err)
	}
}

func TestBusyCanPreemptAndCustomerRevoke(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	_ = s.EnsureMerchant(ctx, m, "test", "secret")
	agent := acquireReq(m, "alice", "agent-1", "event:ars-che")
	insertSess(t, s, m, agent.SessionID, "alice", "agent", "agent-1")
	r1, err := s.Acquire(ctx, agent)
	if err != nil {
		t.Fatal(err)
	}

	browser := acquireReq(m, "alice", "tab", "event:ars-che")
	browser.Principal = lease.Principal{Type: "browser", ID: "tab"}
	insertSess(t, s, m, browser.SessionID, "alice", "browser", "tab")
	busy, err := s.Acquire(ctx, browser)
	if err != nil {
		t.Fatal(err)
	}
	if busy.Status != lease.StatusBusy || !busy.Busy.CanPreempt {
		t.Fatalf("browser busy can_preempt=%v status=%s", busy.Busy, busy.Status)
	}

	e, err := s.Revoke(ctx, m, r1.Execution.ID, browser.SessionID, id.Request(), "take_back")
	if err != nil {
		t.Fatal(err)
	}
	if e.State != lease.StateRevoked {
		t.Fatalf("state=%s", e.State)
	}

	r2, err := s.Acquire(ctx, browser)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Status != lease.StatusGranted {
		t.Fatalf("after revoke status=%s", r2.Status)
	}

	agent2 := id.Session()
	insertSess(t, s, m, agent2, "alice", "agent", "agent-2")
	_, err = s.Revoke(ctx, m, r2.Execution.ID, agent2, id.Request(), "nope")
	if !errors.Is(err, lease.ErrPrecedence) {
		t.Fatalf("agent revoke browser err=%v want ErrPrecedence", err)
	}
}
