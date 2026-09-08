package lease

import (
	"context"
	"sync"
	"testing"
	"time"
)

type fakeStore struct {
	mu     sync.Mutex
	active *Execution
}

func (f *fakeStore) Acquire(_ context.Context, req AcquireRequest) (AcquireResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.active != nil && f.active.ExpiresAt.After(time.Now()) {
		if f.active.Principal.ID == req.Principal.ID {
			e := *f.active
			return AcquireResult{Status: StatusAlreadyHeld, Execution: &e}, nil
		}
		e := *f.active
		return AcquireResult{Status: StatusBusy, Busy: &BusyInfo{
			ActiveExecutionID: e.ID, Holder: e.Principal, ExpiresAt: e.ExpiresAt,
		}}, nil
	}
	e := Execution{
		ID: "exe_1", MerchantID: req.MerchantID, DomainKey: req.DomainKey,
		CustomerID: req.CustomerID, Principal: req.Principal,
		ExpiresAt: time.Now().Add(req.TTL), State: StateActive,
	}
	f.active = &e
	cp := e
	return AcquireResult{Status: StatusGranted, Execution: &cp}, nil
}

func (f *fakeStore) Renew(context.Context, string, string, string, string, time.Duration) (Execution, error) {
	return Execution{}, nil
}
func (f *fakeStore) Release(context.Context, string, string, string, string) (Execution, error) {
	return Execution{}, nil
}
func (f *fakeStore) Revoke(context.Context, string, string, string, string) (Execution, error) {
	return Execution{}, nil
}
func (f *fakeStore) Get(context.Context, string, string) (Execution, error) {
	return Execution{}, nil
}
func (f *fakeStore) ExpireDue(context.Context, int) (int, error) { return 0, nil }
func (f *fakeStore) Ping(context.Context) error                 { return nil }

func TestBusyCacheAbsorbsForeignAcquires(t *testing.T) {
	inner := &fakeStore{}
	c := NewBusyCache(inner, 10_000) // almost never hedge
	ctx := context.Background()
	req := func(agent string) AcquireRequest {
		return AcquireRequest{
			MerchantID: "m", DomainKey: "d", CustomerID: "alice",
			Principal: Principal{Type: "agent", ID: agent}, TTL: time.Minute,
		}
	}
	r1, err := c.Acquire(ctx, req("a1"))
	if err != nil || r1.Status != StatusGranted {
		t.Fatalf("first: %+v %v", r1, err)
	}
	busy := 0
	for i := 0; i < 100; i++ {
		r, err := c.Acquire(ctx, req("other"))
		if err != nil {
			t.Fatal(err)
		}
		if r.Status != StatusBusy {
			t.Fatalf("status %s", r.Status)
		}
		busy++
	}
	if c.Hits() < 90 {
		t.Fatalf("cache hits %d want >=90", c.Hits())
	}
	if busy != 100 {
		t.Fatalf("busy=%d", busy)
	}
}
