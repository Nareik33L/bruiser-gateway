package postgres

import (
	"context"
	"time"
)

const PolicyChannel = "bruiser_policy"

// ListenPolicy blocks on LISTEN bruiser_policy until ctx is cancelled.
// handler is invoked with the merchant_id payload of each notification.
func (s *Store) ListenPolicy(ctx context.Context, handler func(merchantID string)) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return wrapStore(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "listen "+PolicyChannel); err != nil {
		return wrapStore(err)
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		if n == nil {
			continue
		}
		handler(n.Payload)
	}
}

// ListenPolicyLoop reconnects after transient failures until ctx is cancelled.
func (s *Store) ListenPolicyLoop(ctx context.Context, handler func(merchantID string)) {
	for {
		if ctx.Err() != nil {
			return
		}
		err := s.ListenPolicy(ctx, handler)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(400 * time.Millisecond):
			}
		}
	}
}
