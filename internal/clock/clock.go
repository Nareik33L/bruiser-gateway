package clock

import "time"

// Clock is injected everywhere node-local time is read. Lease expiry is
// evaluated with the store's clock (Postgres now() inside the transaction);
// this interface drives timers, token iat, and tests.
type Clock interface {
	Now() time.Time
}

// Real is wall-clock UTC.
type Real struct{}

func (Real) Now() time.Time { return time.Now().UTC() }

// Frozen is a controllable clock for tests.
type Frozen struct {
	t time.Time
}

func NewFrozen(t time.Time) *Frozen { return &Frozen{t: t.UTC()} }

func (f *Frozen) Now() time.Time { return f.t }

func (f *Frozen) Set(t time.Time) { f.t = t.UTC() }

func (f *Frozen) Advance(d time.Duration) { f.t = f.t.Add(d) }
