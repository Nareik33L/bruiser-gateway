package lease

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// BusyCache answers BUSY locally so 10,000 agents do not all hit the store.
// A stale cache can only cause a spurious BUSY (retried), never a duplicate grant.
type BusyCache struct {
	inner Store
	ttl   time.Duration
	hedge uint64 // 1 in hedge requests refresh from the store; 0 disables cache
	now   func() time.Time

	mu    sync.RWMutex
	items map[string]cacheItem
	n     atomic.Uint64
	hits  atomic.Uint64
	miss  atomic.Uint64
}

type cacheItem struct {
	holder    Principal
	exeID     string
	expiresAt time.Time
	maxActive int
	occupied  int
}

func NewBusyCache(inner Store, hedge uint64) *BusyCache {
	if hedge == 0 {
		hedge = 50
	}
	return &BusyCache{
		inner: inner,
		hedge: hedge,
		now:   func() time.Time { return time.Now().UTC() },
		items: map[string]cacheItem{},
	}
}

func cacheKey(merchantID, domainKey string) string { return merchantID + "\x00" + domainKey }

func (c *BusyCache) Reset() {
	c.mu.Lock()
	c.items = map[string]cacheItem{}
	c.mu.Unlock()
}

func (c *BusyCache) remember(merchantID, domainKey string, exe *Execution, maxActive, occupied int) {
	if exe == nil {
		return
	}
	if maxActive < 1 {
		maxActive = 1
	}
	if occupied < 1 {
		occupied = 1
	}
	c.mu.Lock()
	c.items[cacheKey(merchantID, domainKey)] = cacheItem{
		holder:    exe.Principal,
		exeID:     exe.ID,
		expiresAt: exe.ExpiresAt,
		maxActive: maxActive,
		occupied:  occupied,
	}
	c.mu.Unlock()
}

func (c *BusyCache) rememberBusy(merchantID, domainKey string, b *BusyInfo, maxActive int) {
	if b == nil {
		return
	}
	if maxActive < 1 {
		maxActive = 1
	}
	c.mu.Lock()
	c.items[cacheKey(merchantID, domainKey)] = cacheItem{
		holder:    b.Holder,
		exeID:     b.ActiveExecutionID,
		expiresAt: b.ExpiresAt,
		maxActive: maxActive,
		occupied:  maxActive,
	}
	c.mu.Unlock()
}

func (c *BusyCache) forget(merchantID, domainKey string) {
	c.mu.Lock()
	delete(c.items, cacheKey(merchantID, domainKey))
	c.mu.Unlock()
}

func (c *BusyCache) lookup(merchantID, domainKey string) (cacheItem, bool) {
	c.mu.RLock()
	it, ok := c.items[cacheKey(merchantID, domainKey)]
	c.mu.RUnlock()
	if !ok {
		return cacheItem{}, false
	}
	if !it.expiresAt.After(c.now()) {
		c.forget(merchantID, domainKey)
		return cacheItem{}, false
	}
	return it, true
}

func (c *BusyCache) Acquire(ctx context.Context, req AcquireRequest) (AcquireResult, error) {
	n := c.n.Add(1)
	refresh := c.hedge > 0 && n%c.hedge == 0
	// A local BUSY would skip the store and starve the intra-customer queue.
	if req.MaxWaiters > 0 {
		refresh = true
	}
	if !refresh {
		if it, ok := c.lookup(req.MerchantID, req.DomainKey); ok {
			same := it.holder.Type == req.Principal.Type && it.holder.ID == req.Principal.ID
			if !same && it.occupied >= it.maxActive {
				c.hits.Add(1)
				return AcquireResult{
					Status: StatusBusy,
					Busy: &BusyInfo{
						ActiveExecutionID: it.exeID,
						Holder:            it.holder,
						ExpiresAt:         it.expiresAt,
						CanPreempt:        CanPreemptRanked(req.Principal, it.holder, req.Precedence),
					},
				}, nil
			}
		} else {
			c.miss.Add(1)
		}
	}
	res, err := c.inner.Acquire(ctx, req)
	if err != nil {
		return res, err
	}
	ma := req.MaxActive
	if ma < 1 {
		ma = 1
	}
	switch res.Status {
	case StatusGranted:
		occ := 1
		if it, ok := c.lookup(req.MerchantID, req.DomainKey); ok {
			occ = it.occupied + 1
		}
		c.remember(req.MerchantID, req.DomainKey, res.Execution, ma, occ)
	case StatusAlreadyHeld:
		occ := 1
		if it, ok := c.lookup(req.MerchantID, req.DomainKey); ok {
			occ = it.occupied
		}
		c.remember(req.MerchantID, req.DomainKey, res.Execution, ma, occ)
	case StatusBusy:
		c.rememberBusy(req.MerchantID, req.DomainKey, res.Busy, ma)
	case StatusQueued:
		// Do not cache a full domain: a later waiter may still fit.
	}
	return res, nil
}

func (c *BusyCache) Renew(ctx context.Context, merchantID, executionID, sessionID, requestID string, ttl time.Duration) (Execution, error) {
	e, err := c.inner.Renew(ctx, merchantID, executionID, sessionID, requestID, ttl)
	if err == nil {
		c.remember(merchantID, e.DomainKey, &e, 1, 1)
	}
	return e, err
}

func (c *BusyCache) Release(ctx context.Context, merchantID, executionID, sessionID, requestID string) (Execution, error) {
	e, err := c.inner.Release(ctx, merchantID, executionID, sessionID, requestID)
	if err == nil {
		c.forget(merchantID, e.DomainKey)
	}
	return e, err
}

func (c *BusyCache) Revoke(ctx context.Context, merchantID, executionID, sessionID, requestID, reason string) (Execution, error) {
	e, err := c.inner.Revoke(ctx, merchantID, executionID, sessionID, requestID, reason)
	if err == nil {
		c.forget(merchantID, e.DomainKey)
	}
	return e, err
}

func (c *BusyCache) Handoff(ctx context.Context, req HandoffRequest) (HandoffResult, error) {
	res, err := c.inner.Handoff(ctx, req)
	if err == nil {
		c.remember(req.MerchantID, res.Successor.DomainKey, &res.Successor, 1, 1)
	}
	return res, err
}

func (c *BusyCache) Get(ctx context.Context, merchantID, executionID string) (Execution, error) {
	return c.inner.Get(ctx, merchantID, executionID)
}

func (c *BusyCache) ExpireDue(ctx context.Context, limit int) (int, error) {
	return c.inner.ExpireDue(ctx, limit)
}

func (c *BusyCache) Ping(ctx context.Context) error { return c.inner.Ping(ctx) }

func (c *BusyCache) Hits() uint64   { return c.hits.Load() }
func (c *BusyCache) Misses() uint64 { return c.miss.Load() }

var _ Store = (*BusyCache)(nil)
