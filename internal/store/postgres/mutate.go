package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Nareik33L/bruiser-gateway/internal/lease"
)

func (s *Store) lockDomain(ctx context.Context, tx pgx.Tx, merchantID, domainKey string) error {
	var fence int64
	return tx.QueryRow(ctx, `
		select fence from domains
		where merchant_id = $1 and domain_key = $2
		for update`, merchantID, domainKey).Scan(&fence)
}

func (s *Store) loadActiveForUpdate(ctx context.Context, tx pgx.Tx, merchantID, executionID string) (lease.Execution, error) {
	row := tx.QueryRow(ctx, `
		select execution_id, merchant_id, domain_key, customer_id, principal_type, principal_id,
		       session_id, resource, action, rule_name, fence, state, granted_at, expires_at,
		       max_lifetime_at, renew_count, ended_at, end_reason, successor_id
		from executions
		where merchant_id = $1 and execution_id = $2
		for update`, merchantID, executionID)
	e, err := scanExecution(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return lease.Execution{}, lease.ErrNotFound
	}
	if err != nil {
		return lease.Execution{}, err
	}
	if e.State != lease.StateActive {
		return e, &lease.GoneError{Reason: e.State, SuccessorID: e.SuccessorID, ExecutionID: e.ID}
	}
	var live bool
	if err := tx.QueryRow(ctx, `select expires_at > now() from executions where execution_id = $1`, e.ID).Scan(&live); err != nil {
		return e, err
	}
	if !live {
		return e, &lease.GoneError{Reason: lease.ReasonExpired, ExecutionID: e.ID}
	}
	return e, nil
}

// holderMatches allows the original session or a new session bound to the same
// principal, so a reconnect before expiry can heartbeat and release.
func (s *Store) holderMatches(ctx context.Context, e lease.Execution, sessionID string) error {
	if e.SessionID == sessionID {
		return nil
	}
	caller, err := s.Session(ctx, sessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return lease.ErrNotHolder
		}
		return err
	}
	if caller.PrincipalType != e.Principal.Type || caller.PrincipalID != e.Principal.ID {
		return lease.ErrNotHolder
	}
	return nil
}

func (s *Store) lookupDomainKey(ctx context.Context, merchantID, executionID string) (string, error) {
	var domainKey string
	err := s.pool.QueryRow(ctx, `
		select domain_key from executions
		where merchant_id = $1 and execution_id = $2`, merchantID, executionID).Scan(&domainKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", lease.ErrNotFound
	}
	if err != nil {
		return "", wrapStore(err)
	}
	return domainKey, nil
}

func (s *Store) Renew(ctx context.Context, merchantID, executionID, sessionID, requestID string, ttl time.Duration) (lease.Execution, error) {
	domainKey, err := s.lookupDomainKey(ctx, merchantID, executionID)
	if err != nil {
		return lease.Execution{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return lease.Execution{}, wrapStore(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Lock order matches Acquire: domain row first, then execution.
	if err := s.lockDomain(ctx, tx, merchantID, domainKey); err != nil {
		return lease.Execution{}, wrapStore(err)
	}
	e, err := s.loadActiveForUpdate(ctx, tx, merchantID, executionID)
	if err != nil {
		return lease.Execution{}, wrapStore(err)
	}
	if err := s.holderMatches(ctx, e, sessionID); err != nil {
		return lease.Execution{}, err
	}

	if err := tx.QueryRow(ctx, `
		update executions
		set expires_at = least(now() + $2::interval, max_lifetime_at),
		    renew_count = renew_count + 1,
		    session_id = $3
		where execution_id = $1
		returning expires_at, renew_count, session_id`,
		e.ID, interval(ttl), sessionID,
	).Scan(&e.ExpiresAt, &e.RenewCount, &e.SessionID); err != nil {
		return lease.Execution{}, wrapStore(err)
	}

	if err := insertAudit(ctx, tx, merchantID, auditRow{
		typ: "EXECUTION_RENEWED", customerID: e.CustomerID,
		principalType: e.Principal.Type, principalID: e.Principal.ID,
		domainKey: e.DomainKey, executionID: e.ID, ruleName: e.RuleName,
		reason: "heartbeat", requestID: requestID, fence: &e.Fence,
	}); err != nil {
		return lease.Execution{}, wrapStore(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return lease.Execution{}, wrapStore(err)
	}
	return e, nil
}

func (s *Store) Release(ctx context.Context, merchantID, executionID, sessionID, requestID string) (lease.Execution, error) {
	return s.end(ctx, merchantID, executionID, sessionID, requestID, lease.StateReleased, lease.ReasonReleased, true)
}

func (s *Store) Revoke(ctx context.Context, merchantID, executionID, sessionID, requestID, reason string) (lease.Execution, error) {
	if reason == "" {
		reason = lease.ReasonRevoked
	}
	if sessionID == "" {
		return s.end(ctx, merchantID, executionID, "", requestID, lease.StateRevoked, reason, false)
	}
	e, err := s.Get(ctx, merchantID, executionID)
	if err != nil {
		return lease.Execution{}, err
	}
	sess, err := s.Session(ctx, sessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return lease.Execution{}, lease.ErrNotHolder
		}
		return lease.Execution{}, err
	}
	if sess.CustomerID != e.CustomerID {
		return lease.Execution{}, lease.ErrNotFound
	}
	caller := lease.Principal{Type: sess.PrincipalType, ID: sess.PrincipalID}
	if err := s.holderMatches(ctx, e, sessionID); err != nil && !lease.CanPreempt(caller, e.Principal) {
		if errors.Is(err, lease.ErrNotHolder) {
			return lease.Execution{}, lease.ErrPrecedence
		}
		return lease.Execution{}, err
	}
	return s.end(ctx, merchantID, executionID, "", requestID, lease.StateRevoked, reason, false)
}

func (s *Store) end(ctx context.Context, merchantID, executionID, sessionID, requestID, newState, reason string, mustHolder bool) (lease.Execution, error) {
	domainKey, err := s.lookupDomainKey(ctx, merchantID, executionID)
	if err != nil {
		return lease.Execution{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return lease.Execution{}, wrapStore(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.lockDomain(ctx, tx, merchantID, domainKey); err != nil {
		return lease.Execution{}, wrapStore(err)
	}
	e, err := s.loadActiveForUpdate(ctx, tx, merchantID, executionID)
	if err != nil {
		return lease.Execution{}, wrapStore(err)
	}
	if mustHolder {
		if err := s.holderMatches(ctx, e, sessionID); err != nil {
			return lease.Execution{}, err
		}
	}

	var endedAt time.Time
	if err := tx.QueryRow(ctx, `
		update executions
		set state = $2, ended_at = now(), end_reason = $3
		where execution_id = $1
		returning ended_at`, e.ID, newState, reason,
	).Scan(&endedAt); err != nil {
		return lease.Execution{}, wrapStore(err)
	}
	e.State = newState
	e.EndedAt = &endedAt
	e.EndReason = reason

	auditType := "EXECUTION_RELEASED"
	if newState == lease.StateRevoked {
		auditType = "EXECUTION_REVOKED"
	}
	if err := insertAudit(ctx, tx, merchantID, auditRow{
		typ: auditType, customerID: e.CustomerID,
		principalType: e.Principal.Type, principalID: e.Principal.ID,
		domainKey: e.DomainKey, executionID: e.ID, ruleName: e.RuleName,
		reason: reason, requestID: requestID, fence: &e.Fence,
	}); err != nil {
		return lease.Execution{}, wrapStore(err)
	}
	if err := s.promoteLocked(ctx, tx, merchantID, domainKey, requestID); err != nil {
		return lease.Execution{}, wrapStore(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return lease.Execution{}, wrapStore(err)
	}
	return e, nil
}

func (s *Store) ExpireDue(ctx context.Context, limit int) (int, error) {
	if limit < 1 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, `
		select distinct merchant_id, domain_key
		from executions
		where state = 'ACTIVE' and expires_at <= now()
		limit $1`, limit)
	if err != nil {
		return 0, wrapStore(err)
	}
	type pair struct{ merchant, domain string }
	var domains []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.merchant, &p.domain); err != nil {
			rows.Close()
			return 0, wrapStore(err)
		}
		domains = append(domains, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, wrapStore(err)
	}

	n := 0
	for _, p := range domains {
		k, err := s.expireAndPromoteDomain(ctx, p.merchant, p.domain)
		if err != nil {
			return n, err
		}
		n += k
	}
	return n, nil
}

func (s *Store) expireAndPromoteDomain(ctx context.Context, merchantID, domainKey string) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, wrapStore(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.lockDomain(ctx, tx, merchantID, domainKey); err != nil {
		return 0, wrapStore(err)
	}

	rows, err := tx.Query(ctx, `
		select execution_id, customer_id, principal_type, principal_id, rule_name, fence
		from executions
		where merchant_id = $1 and domain_key = $2
		  and state = 'ACTIVE' and expires_at <= now()`, merchantID, domainKey)
	if err != nil {
		return 0, wrapStore(err)
	}
	type due struct {
		id, customer, ptype, pid, rule string
		fence                          int64
	}
	var dues []due
	for rows.Next() {
		var d due
		if err := rows.Scan(&d.id, &d.customer, &d.ptype, &d.pid, &d.rule, &d.fence); err != nil {
			rows.Close()
			return 0, wrapStore(err)
		}
		dues = append(dues, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, wrapStore(err)
	}

	for _, d := range dues {
		if _, err := tx.Exec(ctx, `
			update executions
			set state = 'EXPIRED', ended_at = now(), end_reason = 'EXPIRED'
			where execution_id = $1 and state = 'ACTIVE'`, d.id); err != nil {
			return 0, wrapStore(err)
		}
		fence := d.fence
		if err := insertAudit(ctx, tx, merchantID, auditRow{
			typ: "EXECUTION_EXPIRED", customerID: d.customer,
			principalType: d.ptype, principalID: d.pid,
			domainKey: domainKey, executionID: d.id, ruleName: d.rule,
			reason: "expired", fence: &fence,
		}); err != nil {
			return 0, wrapStore(err)
		}
	}
	if err := s.promoteLocked(ctx, tx, merchantID, domainKey, ""); err != nil {
		return 0, wrapStore(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, wrapStore(err)
	}
	return len(dues), nil
}

func (s *Store) CountAudit(ctx context.Context, merchantID, typ string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		select count(*) from audit_events where merchant_id = $1 and type = $2`,
		merchantID, typ).Scan(&n)
	return n, wrapStore(err)
}
