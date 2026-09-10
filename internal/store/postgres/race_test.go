package postgres_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/id"
	"github.com/Nareik33L/bruiser-gateway/internal/lease"
)

func TestConcurrentAcquireReleaseExactlyOneActive(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	if err := s.EnsureMerchant(ctx, m, "test", "secret"); err != nil {
		t.Fatal(err)
	}
	first := acquireReq(m, "alice", "holder", "event:race-ar")
	insertSess(t, s, m, first.SessionID, "alice", "agent", "holder")
	g, err := s.Acquire(ctx, first)
	if err != nil || g.Status != lease.StatusGranted {
		t.Fatalf("seed: %+v %v", g, err)
	}

	const n = 80
	var granted atomic.Int64
	var wg sync.WaitGroup
	wg.Add(n + 1)
	go func() {
		defer wg.Done()
		_, _ = s.Release(ctx, m, g.Execution.ID, first.SessionID, id.Request())
	}()
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			req := acquireReq(m, "alice", id.New("ag"), "event:race-ar")
			r, err := s.Acquire(ctx, req)
			if err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			if r.Status == lease.StatusGranted {
				granted.Add(1)
			}
		}()
		_ = i
	}
	wg.Wait()
	if granted.Load() > 1 {
		t.Fatalf("granted=%d — release must not create two actives", granted.Load())
	}
	active, err := s.ListActive(ctx, m, "alice", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) > 1 {
		t.Fatalf("active executions=%d want <=1", len(active))
	}
}

func TestConcurrentRenewAndRevoke(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	_ = s.EnsureMerchant(ctx, m, "test", "secret")
	req := acquireReq(m, "alice", "agent-1", "event:race-rr")
	insertSess(t, s, m, req.SessionID, "alice", "agent", "agent-1")
	g, err := s.Acquire(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	revoker := id.Session()
	insertSess(t, s, m, revoker, "alice", "browser", "tab")

	var wg sync.WaitGroup
	wg.Add(2)
	var renewErr, revokeErr error
	go func() {
		defer wg.Done()
		_, renewErr = s.Renew(ctx, m, g.Execution.ID, req.SessionID, id.Request(), 5*time.Second)
	}()
	go func() {
		defer wg.Done()
		_, revokeErr = s.Revoke(ctx, m, g.Execution.ID, revoker, id.Request(), "race")
	}()
	wg.Wait()
	if renewErr == nil && revokeErr == nil {
		// Possible if renew won then revoke applied. Final state must not be two actives.
	}
	got, err := s.Get(ctx, m, g.Execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	switch got.State {
	case lease.StateActive, lease.StateRevoked:
	default:
		t.Fatalf("state=%s", got.State)
	}
	if got.State == lease.StateActive && revokeErr == nil {
		t.Fatal("revoke reported success but execution still ACTIVE")
	}
	active, err := s.ListActive(ctx, m, "alice", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) > 1 {
		t.Fatalf("active=%d", len(active))
	}
}

func TestExpiryVersusAcquireExactlyOne(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	_ = s.EnsureMerchant(ctx, m, "test", "secret")
	req := acquireReq(m, "alice", "agent-1", "event:race-exp")
	req.TTL = 80 * time.Millisecond
	g, err := s.Acquire(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	_ = g
	time.Sleep(100 * time.Millisecond)

	const n = 40
	var granted atomic.Int64
	var wg sync.WaitGroup
	wg.Add(n + 1)
	go func() {
		defer wg.Done()
		_, _ = s.ExpireDue(ctx, 50)
	}()
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			r, err := s.Acquire(ctx, acquireReq(m, "alice", id.New("ag"), "event:race-exp"))
			if err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			if r.Status == lease.StatusGranted {
				granted.Add(1)
			}
		}()
	}
	wg.Wait()
	if granted.Load() != 1 {
		t.Fatalf("granted=%d want 1 after expiry race", granted.Load())
	}
}

func TestDuplicateRequestIdempotent(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	_ = s.EnsureMerchant(ctx, m, "test", "secret")
	req := acquireReq(m, "alice", "agent-1", "event:dup")
	const n = 40
	results := make([]lease.AcquireResult, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			var err error
			results[i], err = s.Acquire(ctx, req)
			if err != nil {
				t.Errorf("dup acquire: %v", err)
			}
		}()
	}
	wg.Wait()
	granted, held := 0, 0
	var exe string
	for _, r := range results {
		switch r.Status {
		case lease.StatusGranted:
			granted++
			exe = r.Execution.ID
		case lease.StatusAlreadyHeld:
			held++
			if exe == "" {
				exe = r.Execution.ID
			} else if r.Execution.ID != exe {
				t.Fatalf("duplicate request returned different executions")
			}
		default:
			t.Fatalf("status=%s", r.Status)
		}
	}
	if granted+held != n || granted > 1 {
		t.Fatalf("granted=%d held=%d", granted, held)
	}
}

func TestConcurrentWaitersPromoteOne(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	_ = s.EnsureMerchant(ctx, m, "test", "secret")
	first := acquireReq(m, "alice", "holder", "event:race-q")
	insertSess(t, s, m, first.SessionID, "alice", "agent", "holder")
	g, err := s.Acquire(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	const waiters = 8
	var queued atomic.Int64
	var wg sync.WaitGroup
	wg.Add(waiters)
	for i := 0; i < waiters; i++ {
		go func() {
			defer wg.Done()
			req := acquireReq(m, "alice", id.New("w"), "event:race-q")
			req.MaxWaiters = waiters
			r, err := s.Acquire(ctx, req)
			if err != nil {
				t.Errorf("queue: %v", err)
				return
			}
			if r.Status == lease.StatusQueued {
				queued.Add(1)
			}
		}()
	}
	wg.Wait()
	if queued.Load() < 1 {
		t.Fatalf("queued=%d", queued.Load())
	}
	if _, err := s.Release(ctx, m, g.Execution.ID, first.SessionID, id.Request()); err != nil {
		t.Fatal(err)
	}
	active, err := s.ListActive(ctx, m, "alice", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("after release active=%d want 1", len(active))
	}
}

func TestConcurrentHandoffOneSuccessor(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	_ = s.EnsureMerchant(ctx, m, "test", "secret")
	agent := acquireReq(m, "alice", "agent-1", "event:race-h")
	insertSess(t, s, m, agent.SessionID, "alice", "agent", "agent-1")
	g, err := s.Acquire(ctx, agent)
	if err != nil {
		t.Fatal(err)
	}
	const n = 8
	var ok atomic.Int64
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			sess := id.Session()
			insertSess(t, s, m, sess, "alice", "browser", id.New("tab"))
			_, err := s.Handoff(ctx, lease.HandoffRequest{
				MerchantID:  m,
				ExecutionID: g.Execution.ID,
				SessionID:   sess,
				Mode:        lease.ModePreempt,
				TTL:         5 * time.Second,
				RequestID:   id.Request(),
			})
			if err == nil {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() < 1 {
		t.Fatal("expected at least one handoff to succeed")
	}
	active, err := s.ListActive(ctx, m, "alice", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("handoff race left %d actives", len(active))
	}
}
