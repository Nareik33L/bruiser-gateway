package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/id"
	"github.com/Nareik33L/bruiser-gateway/internal/lease"
)

func TestBoundedQueuePromoteOnRelease(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	if err := s.EnsureMerchant(ctx, m, "test", "secret"); err != nil {
		t.Fatal(err)
	}

	first := acquireReq(m, "alice", "agent-1", "event:queue")
	g, err := s.Acquire(ctx, first)
	if err != nil || g.Status != lease.StatusGranted {
		t.Fatalf("grant: %+v %v", g, err)
	}

	queued := acquireReq(m, "alice", "agent-2", "event:queue")
	queued.MaxWaiters = 1
	q, err := s.Acquire(ctx, queued)
	if err != nil || q.Status != lease.StatusQueued {
		t.Fatalf("queue: %+v %v", q, err)
	}
	if q.Queue == nil || q.Queue.Position != 1 {
		t.Fatalf("position %+v", q.Queue)
	}

	again, err := s.Acquire(ctx, queued)
	if err != nil || again.Status != lease.StatusQueued || again.Queue.WaiterID != q.Queue.WaiterID {
		t.Fatalf("requeue: %+v %v", again, err)
	}

	busy := acquireReq(m, "alice", "agent-3", "event:queue")
	busy.MaxWaiters = 1
	b, err := s.Acquire(ctx, busy)
	if err != nil || b.Status != lease.StatusBusy {
		t.Fatalf("third want BUSY got %+v %v", b, err)
	}

	got, err := s.Get(ctx, m, q.Queue.WaiterID)
	if err != nil || got.State != lease.StateQueued {
		t.Fatalf("get waiter: %+v %v", got, err)
	}

	if _, err := s.Release(ctx, m, g.Execution.ID, first.SessionID, id.Request()); err != nil {
		t.Fatal(err)
	}

	promoted, err := s.Get(ctx, m, q.Queue.WaiterID)
	if err != nil || promoted.State != lease.StateActive {
		t.Fatalf("promoted: %+v %v", promoted, err)
	}
	if promoted.Principal.ID != "agent-2" {
		t.Fatalf("holder %s", promoted.Principal.ID)
	}
	n, err := s.CountAudit(ctx, m, "EXECUTION_QUEUED")
	if err != nil || n < 1 {
		t.Fatalf("queued audits %d %v", n, err)
	}
}

func TestQueueDoesNotLineUpOtherCustomers(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	if err := s.EnsureMerchant(ctx, m, "test", "secret"); err != nil {
		t.Fatal(err)
	}

	alice := acquireReq(m, "alice", "agent-1", "event:derby")
	g, err := s.Acquire(ctx, alice)
	if err != nil || g.Status != lease.StatusGranted {
		t.Fatalf("alice grant: %+v %v", g, err)
	}

	alice2 := acquireReq(m, "alice", "agent-2", "event:derby")
	alice2.MaxWaiters = 1
	q, err := s.Acquire(ctx, alice2)
	if err != nil || q.Status != lease.StatusQueued {
		t.Fatalf("alice waiter: %+v %v", q, err)
	}

	// Bob is a different customer on the same resource: different domain.
	// He must GRANT immediately — not sit behind Alice.
	bob := acquireReq(m, "bob", "agent-1", "event:derby")
	bob.MaxWaiters = 1
	bg, err := s.Acquire(ctx, bob)
	if err != nil || bg.Status != lease.StatusGranted {
		t.Fatalf("bob must GRANT independently, got %+v %v", bg, err)
	}
	if bg.Execution.ID == g.Execution.ID || bg.Execution.DomainKey == g.Execution.DomainKey {
		t.Fatalf("bob shared alice domain %s", bg.Execution.DomainKey)
	}

	if _, err := s.Release(ctx, m, g.Execution.ID, alice.SessionID, id.Request()); err != nil {
		t.Fatal(err)
	}
	promoted, err := s.Get(ctx, m, q.Queue.WaiterID)
	if err != nil || promoted.State != lease.StateActive || promoted.CustomerID != "alice" {
		t.Fatalf("alice waiter should proceed: %+v %v", promoted, err)
	}
	still, err := s.Get(ctx, m, bg.Execution.ID)
	if err != nil || still.State != lease.StateActive || still.CustomerID != "bob" {
		t.Fatalf("bob must stay ACTIVE: %+v %v", still, err)
	}
}

func TestLeaveQueue(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	if err := s.EnsureMerchant(ctx, m, "test", "secret"); err != nil {
		t.Fatal(err)
	}
	first := acquireReq(m, "alice", "agent-1", "event:leave")
	if _, err := s.Acquire(ctx, first); err != nil {
		t.Fatal(err)
	}
	queued := acquireReq(m, "alice", "agent-2", "event:leave")
	queued.MaxWaiters = 1
	q, err := s.Acquire(ctx, queued)
	if err != nil || q.Status != lease.StatusQueued {
		t.Fatalf("queue: %+v %v", q, err)
	}
	left, err := s.Release(ctx, m, q.Queue.WaiterID, queued.SessionID, id.Request())
	if err != nil || left.State != lease.StateReleased {
		t.Fatalf("leave: %+v %v", left, err)
	}
	if _, err := s.Get(ctx, m, q.Queue.WaiterID); err == nil {
		t.Fatal("left waiter should be gone")
	}
	third := acquireReq(m, "alice", "agent-3", "event:leave")
	third.MaxWaiters = 1
	again, err := s.Acquire(ctx, third)
	if err != nil || again.Status != lease.StatusQueued {
		t.Fatalf("slot should reopen: %+v %v", again, err)
	}
}

func TestExpireWaiters(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	if err := s.EnsureMerchant(ctx, m, "test", "secret"); err != nil {
		t.Fatal(err)
	}
	first := acquireReq(m, "alice", "agent-1", "event:qttl")
	if _, err := s.Acquire(ctx, first); err != nil {
		t.Fatal(err)
	}
	queued := acquireReq(m, "alice", "agent-2", "event:qttl")
	queued.MaxWaiters = 1
	queued.MaxLifetime = 50 * time.Millisecond
	q, err := s.Acquire(ctx, queued)
	if err != nil || q.Status != lease.StatusQueued {
		t.Fatalf("queue: %+v %v", q, err)
	}
	time.Sleep(80 * time.Millisecond)
	n, err := s.ExpireWaiters(ctx, 50)
	if err != nil || n < 1 {
		t.Fatalf("expire waiters n=%d err=%v", n, err)
	}
	if _, err := s.Get(ctx, m, q.Queue.WaiterID); err == nil {
		t.Fatal("expired waiter should be gone")
	}
	if n, err := s.CountAudit(ctx, m, "EXECUTION_QUEUE_EXPIRED"); err != nil || n < 1 {
		t.Fatalf("queue expired audits %d %v", n, err)
	}
}

func TestBoundedQueuePromoteOnExpire(t *testing.T) {
	s := connect(t)
	ctx := context.Background()
	m := id.New("m")
	if err := s.EnsureMerchant(ctx, m, "test", "secret"); err != nil {
		t.Fatal(err)
	}
	first := acquireReq(m, "alice", "agent-1", "event:qexp")
	first.TTL = 80 * time.Millisecond
	g, err := s.Acquire(ctx, first)
	if err != nil || g.Status != lease.StatusGranted {
		t.Fatalf("grant: %+v %v", g, err)
	}
	queued := acquireReq(m, "alice", "agent-2", "event:qexp")
	queued.MaxWaiters = 1
	q, err := s.Acquire(ctx, queued)
	if err != nil || q.Status != lease.StatusQueued {
		t.Fatalf("queue: %+v %v", q, err)
	}
	time.Sleep(120 * time.Millisecond)
	if _, err := s.ExpireDue(ctx, 50); err != nil {
		t.Fatal(err)
	}
	promoted, err := s.Get(ctx, m, q.Queue.WaiterID)
	if err != nil || promoted.State != lease.StateActive {
		t.Fatalf("promoted after expire: %+v %v", promoted, err)
	}
}
