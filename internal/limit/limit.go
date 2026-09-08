// Package limit enforces per-execution in-flight and rate caps (design §9.4).
package limit

import (
	"sync"
	"time"
)

type PerExecution struct {
	MaxInFlight int
	RPS         float64
	Burst       int

	mu   sync.Mutex
	byID map[string]*slot
}

type slot struct {
	inflight int
	tokens   float64
	last     time.Time
}

func New(maxInFlight int, rps float64) *PerExecution {
	if maxInFlight <= 0 {
		maxInFlight = 2
	}
	if rps <= 0 {
		rps = 5
	}
	burst := int(rps) + 5
	if burst < 1 {
		burst = 10
	}
	return &PerExecution{
		MaxInFlight: maxInFlight,
		RPS:         rps,
		Burst:       burst,
		byID:        map[string]*slot{},
	}
}

// Take reserves one in-flight slot and one token. ok is false on 429.
func (p *PerExecution) Take(executionID string, now time.Time) (release func(), ok bool) {
	if executionID == "" {
		return func() {}, true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.byID[executionID]
	if s == nil {
		s = &slot{tokens: p.RPS, last: now}
		p.byID[executionID] = s
	}
	elapsed := now.Sub(s.last).Seconds()
	if elapsed > 0 {
		s.tokens += elapsed * p.RPS
		cap := p.RPS
		if p.Burst > 0 {
			cap = float64(p.Burst)
		}
		if s.tokens > cap {
			s.tokens = cap
		}
		s.last = now
	}
	if s.inflight >= p.MaxInFlight || s.tokens < 1 {
		return func() {}, false
	}
	s.inflight++
	s.tokens--
	return func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		s.inflight--
		if s.inflight <= 0 && s.tokens >= p.RPS-0.01 {
			delete(p.byID, executionID)
		}
	}, true
}
