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
