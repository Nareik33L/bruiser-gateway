package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Nareik33L/bruiser-gateway/internal/lease"
)

var (
	ErrSessionRevoked = errors.New("session revoked")
	ErrReplay         = errors.New("replayed request")
	ErrBudget         = errors.New("execution budget exhausted")
)

func (s *Store) RevokeSession(ctx context.Context, merchantID, sessionID string) error {
	tag, err := s.pool.Exec(ctx, `
		update sessions
		set revoked_at = now(), version = version + 1
		where session_id = $1 and merchant_id = $2 and revoked_at is null`,
		sessionID, merchantID)
	if err != nil {
		return wrapStore(err)
	}
	if tag.RowsAffected() == 0 {
		return lease.ErrNotFound
	}
	return nil
}

func (s *Store) LiveSession(ctx context.Context, merchantID, sessionID string) (SessionRow, error) {
	sess, err := s.Session(ctx, sessionID)
	if err != nil {
		return sess, err
	}
	if sess.MerchantID != merchantID {
		return sess, lease.ErrForbidden
	}
	if sess.RevokedAt != nil {
		return sess, ErrSessionRevoked
	}
	if !sess.ExpiresAt.After(time.Now().UTC()) {
		return sess, ErrSessionRevoked
	}
	return sess, nil
}

func (s *Store) EnsureBudget(ctx context.Context, merchantID, executionID string, maxOps int) error {
	_, err := s.pool.Exec(ctx, `
		insert into execution_budget (execution_id, merchant_id, max_ops, ops_used)
		values ($1, $2, $3, 0)
		on conflict (execution_id) do update
		  set max_ops = case when excluded.max_ops > execution_budget.max_ops
		                     then excluded.max_ops else execution_budget.max_ops end`,
		executionID, merchantID, maxOps)
	return wrapStore(err)
}

func (s *Store) ConsumeOp(ctx context.Context, merchantID, executionID string) (used, max int, err error) {
	err = s.pool.QueryRow(ctx, `
		insert into execution_budget (execution_id, merchant_id, max_ops, ops_used)
		values ($1, $2, 0, 1)
		on conflict (execution_id) do update
		  set ops_used = execution_budget.ops_used + 1
		where execution_budget.max_ops = 0
		   or execution_budget.ops_used < execution_budget.max_ops
		returning ops_used, max_ops`,
		executionID, merchantID).Scan(&used, &max)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, ErrBudget
	}
	if err != nil {
		return 0, 0, wrapStore(err)
	}
	return used, max, nil
}

// ClaimReplay records a one-shot key. A duplicate inside the window returns ErrReplay.
func (s *Store) ClaimReplay(ctx context.Context, merchantID, key, kind string, window time.Duration) error {
	if key == "" {
		return nil
	}
	if window <= 0 {
		window = 10 * time.Minute
	}
	_, err := s.pool.Exec(ctx, `delete from replay_keys where expires_at < now()`)
	if err != nil {
		return wrapStore(err)
	}
	tag, err := s.pool.Exec(ctx, `
		insert into replay_keys (merchant_id, replay_key, kind, expires_at)
		values ($1, $2, $3, now() + $4::interval)
		on conflict (merchant_id, replay_key) do nothing`,
		merchantID, key, kind, interval(window))
	if err != nil {
		return wrapStore(err)
	}
	if tag.RowsAffected() == 0 {
		return ErrReplay
	}
	return nil
}
