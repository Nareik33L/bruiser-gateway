package postgres

// Waiters are intra-customer only. The domain key already includes
// customer=, so Alice's agents never share a line with Bob. One customer
// has one ACTIVE execution; extra agents of that customer may wait; when
// that execution ends the next permitted one proceeds. This is not a
// waiting room.

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Nareik33L/bruiser-gateway/internal/id"
	"github.com/Nareik33L/bruiser-gateway/internal/lease"
)

type waiterRow struct {
	ID          string
	MerchantID  string
	DomainKey   string
	CustomerID  string
	Principal   lease.Principal
	SessionID   string
	Resource    string
	Action      string
	RuleName    string
	MaxActive   int
	LeaseTTL    time.Duration
	MaxLifetime time.Duration
	CreatedAt   time.Time
	ExpiresAt   time.Time
	Position    int
}

func (s *Store) enqueueWaiter(ctx context.Context, tx pgx.Tx, req lease.AcquireRequest, holder lease.Execution) (lease.AcquireResult, error) {
	existing, err := loadWaiterByPrincipal(ctx, tx, req.MerchantID, req.DomainKey, req.Principal)
	if err == nil {
		pos, perr := waiterPosition(ctx, tx, req.MerchantID, req.DomainKey, existing.CreatedAt)
		if perr != nil {
			return lease.AcquireResult{}, wrapStore(perr)
		}
		existing.Position = pos
		if err := tx.Commit(ctx); err != nil {
			return lease.AcquireResult{}, wrapStore(err)
		}
		return queuedResult(existing, holder), nil
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return lease.AcquireResult{}, wrapStore(err)
	}

	var n int
	if err := tx.QueryRow(ctx, `
		select count(*) from waiters
		where merchant_id = $1 and domain_key = $2 and expires_at > now()`,
		req.MerchantID, req.DomainKey).Scan(&n); err != nil {
		return lease.AcquireResult{}, wrapStore(err)
	}
	if n >= req.MaxWaiters {
		return busyResult(ctx, tx, req, holder)
	}

	w := waiterRow{
		ID:         id.Execution(),
		MerchantID: req.MerchantID,
		DomainKey:  req.DomainKey,
		CustomerID: req.CustomerID,
		Principal:  req.Principal,
		SessionID:  req.SessionID,
		Resource:   req.Resource,
		Action:     req.Action,
		RuleName:   req.RuleName,
		MaxActive:  req.MaxActive,
	}
	w.LeaseTTL = req.TTL
	w.MaxLifetime = req.MaxLifetime
	if err := tx.QueryRow(ctx, `
		insert into waiters (
			waiter_id, merchant_id, domain_key, customer_id, principal_type, principal_id,
			session_id, resource, action, rule_name, max_active, lease_ttl_ms, max_lifetime_ms, expires_at
		) values (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13, now() + $14::interval
		)
		returning created_at, expires_at`,
		w.ID, req.MerchantID, req.DomainKey, req.CustomerID,
		req.Principal.Type, req.Principal.ID, req.SessionID,
		req.Resource, req.Action, req.RuleName, req.MaxActive,
		req.TTL.Milliseconds(), req.MaxLifetime.Milliseconds(),
		interval(req.MaxLifetime),
	).Scan(&w.CreatedAt, &w.ExpiresAt); err != nil {
		return lease.AcquireResult{}, wrapStore(err)
	}
	pos, err := waiterPosition(ctx, tx, req.MerchantID, req.DomainKey, w.CreatedAt)
	if err != nil {
		return lease.AcquireResult{}, wrapStore(err)
	}
	w.Position = pos
	if err := insertAudit(ctx, tx, req.MerchantID, auditRow{
		typ: "EXECUTION_QUEUED", customerID: req.CustomerID,
		principalType: req.Principal.Type, principalID: req.Principal.ID,
		domainKey: req.DomainKey, executionID: w.ID, ruleName: req.RuleName,
		reason: "queued", requestID: req.RequestID, fence: &holder.Fence,
	}); err != nil {
		return lease.AcquireResult{}, wrapStore(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return lease.AcquireResult{}, wrapStore(err)
	}
	return queuedResult(w, holder), nil
}

func busyResult(ctx context.Context, tx pgx.Tx, req lease.AcquireRequest, holder lease.Execution) (lease.AcquireResult, error) {
	if err := insertAudit(ctx, tx, req.MerchantID, auditRow{
		typ: "EXECUTION_BUSY", customerID: req.CustomerID,
		principalType: req.Principal.Type, principalID: req.Principal.ID,
		domainKey: req.DomainKey, executionID: holder.ID, ruleName: req.RuleName,
		reason: "max_active", requestID: req.RequestID, fence: &holder.Fence,
	}); err != nil {
		return lease.AcquireResult{}, wrapStore(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return lease.AcquireResult{}, wrapStore(err)
	}
	return lease.AcquireResult{
		Status: lease.StatusBusy,
		Busy: &lease.BusyInfo{
			ActiveExecutionID: holder.ID,
			Holder:            holder.Principal,
			ExpiresAt:         holder.ExpiresAt,
			CanPreempt:        lease.CanPreemptRanked(req.Principal, holder.Principal, req.Precedence),
		},
	}, nil
}

func queuedResult(w waiterRow, holder lease.Execution) lease.AcquireResult {
	exe := waiterAsExecution(w)
	return lease.AcquireResult{
		Status:    lease.StatusQueued,
		Execution: &exe,
		Busy: &lease.BusyInfo{
			ActiveExecutionID: holder.ID,
			Holder:            holder.Principal,
			ExpiresAt:         holder.ExpiresAt,
		},
		Queue: &lease.QueueInfo{
			WaiterID:          w.ID,
			Position:          w.Position,
			ActiveExecutionID: holder.ID,
			ExpiresAt:         w.ExpiresAt,
		},
	}
}

func waiterAsExecution(w waiterRow) lease.Execution {
	return lease.Execution{
		ID:            w.ID,
		MerchantID:    w.MerchantID,
		DomainKey:     w.DomainKey,
		CustomerID:    w.CustomerID,
		Principal:     w.Principal,
		SessionID:     w.SessionID,
		Resource:      w.Resource,
		Action:        w.Action,
		RuleName:      w.RuleName,
		State:         lease.StateQueued,
		GrantedAt:     w.CreatedAt,
		ExpiresAt:     w.ExpiresAt,
		MaxLifetimeAt: w.ExpiresAt,
		QueuePosition: w.Position,
	}
}

func loadWaiterByPrincipal(ctx context.Context, tx pgx.Tx, merchantID, domainKey string, p lease.Principal) (waiterRow, error) {
	row := tx.QueryRow(ctx, waiterSelect+`
		where merchant_id = $1 and domain_key = $2
		  and principal_type = $3 and principal_id = $4
		  and expires_at > now()`,
		merchantID, domainKey, p.Type, p.ID)
	return scanWaiter(row)
}

type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func waiterPosition(ctx context.Context, q queryRower, merchantID, domainKey string, createdAt time.Time) (int, error) {
	var n int
	err := q.QueryRow(ctx, `
		select count(*) from waiters
		where merchant_id = $1 and domain_key = $2
		  and expires_at > now() and created_at <= $3`,
		merchantID, domainKey, createdAt).Scan(&n)
	return n, err
}

const waiterSelect = `
		select waiter_id, merchant_id, domain_key, customer_id, principal_type, principal_id,
		       session_id, resource, action, rule_name, max_active, lease_ttl_ms, max_lifetime_ms,
		       created_at, expires_at
		from waiters`

func scanWaiter(row rowScanner) (waiterRow, error) {
	var w waiterRow
	var ttlMS, lifeMS int64
	err := row.Scan(
		&w.ID, &w.MerchantID, &w.DomainKey, &w.CustomerID, &w.Principal.Type, &w.Principal.ID,
		&w.SessionID, &w.Resource, &w.Action, &w.RuleName, &w.MaxActive, &ttlMS, &lifeMS,
		&w.CreatedAt, &w.ExpiresAt,
	)
	w.LeaseTTL = time.Duration(ttlMS) * time.Millisecond
	w.MaxLifetime = time.Duration(lifeMS) * time.Millisecond
	return w, err
}

func (s *Store) getWaiter(ctx context.Context, merchantID, waiterID string) (lease.Execution, error) {
	row := s.pool.QueryRow(ctx, waiterSelect+`
		where merchant_id = $1 and waiter_id = $2 and expires_at > now()`, merchantID, waiterID)
	w, err := scanWaiter(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return lease.Execution{}, lease.ErrNotFound
	}
	if err != nil {
		return lease.Execution{}, wrapStore(err)
	}
	pos, err := waiterPosition(ctx, s.pool, merchantID, w.DomainKey, w.CreatedAt)
	if err != nil {
		return lease.Execution{}, wrapStore(err)
	}
	w.Position = pos
	return waiterAsExecution(w), nil
}

func (s *Store) promoteLocked(ctx context.Context, tx pgx.Tx, merchantID, domainKey, requestID string) error {
	if _, err := tx.Exec(ctx, `
		delete from waiters
		where merchant_id = $1 and domain_key = $2 and expires_at <= now()`,
		merchantID, domainKey); err != nil {
		return err
	}

	var active int
	if err := tx.QueryRow(ctx, `
		select count(*) from executions
		where merchant_id = $1 and domain_key = $2 and state = 'ACTIVE' and expires_at > now()`,
		merchantID, domainKey).Scan(&active); err != nil {
		return err
	}

	row := tx.QueryRow(ctx, waiterSelect+`
		where merchant_id = $1 and domain_key = $2 and expires_at > now()
		order by created_at
		limit 1
		for update skip locked`, merchantID, domainKey)
	w, err := scanWaiter(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if w.MaxActive < 1 {
		w.MaxActive = 1
	}
	if active >= w.MaxActive {
		return nil
	}

	var fence int64
	if err := tx.QueryRow(ctx, `
		update domains set fence = fence + 1, updated_at = now()
		where merchant_id = $1 and domain_key = $2
		returning fence`, merchantID, domainKey).Scan(&fence); err != nil {
		return err
	}

	ttl := w.LeaseTTL
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	life := w.MaxLifetime
	if life <= 0 {
		life = 15 * time.Minute
	}
	if _, err := tx.Exec(ctx, `
		insert into executions (
			execution_id, merchant_id, domain_key, customer_id, principal_type, principal_id,
			session_id, resource, action, rule_name, fence, state,
			granted_at, expires_at, max_lifetime_at, renew_count
		) values (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'ACTIVE',
			now(), now() + $12::interval, now() + $13::interval, 0
		)`,
		w.ID, w.MerchantID, w.DomainKey, w.CustomerID,
		w.Principal.Type, w.Principal.ID, w.SessionID,
		w.Resource, w.Action, w.RuleName, fence,
		interval(ttl), interval(life),
	); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `delete from waiters where waiter_id = $1`, w.ID); err != nil {
		return err
	}
	return insertAudit(ctx, tx, merchantID, auditRow{
		typ: "EXECUTION_GRANTED", customerID: w.CustomerID,
		principalType: w.Principal.Type, principalID: w.Principal.ID,
		domainKey: domainKey, executionID: w.ID, ruleName: w.RuleName,
		reason: "promoted", requestID: requestID, fence: &fence,
	})
}

func (s *Store) promoteDue(ctx context.Context, merchantID, domainKey string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return wrapStore(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := s.lockDomain(ctx, tx, merchantID, domainKey); err != nil {
		return wrapStore(err)
	}
	if err := s.promoteLocked(ctx, tx, merchantID, domainKey, ""); err != nil {
		return wrapStore(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return wrapStore(err)
	}
	return nil
}

func (s *Store) dequeue(ctx context.Context, merchantID, waiterID, sessionID, requestID string) (lease.Execution, error) {
	domainKey, err := s.lookupDomainKey(ctx, merchantID, waiterID)
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
	row := tx.QueryRow(ctx, waiterSelect+`
		where merchant_id = $1 and waiter_id = $2
		for update`, merchantID, waiterID)
	w, err := scanWaiter(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return lease.Execution{}, lease.ErrNotFound
	}
	if err != nil {
		return lease.Execution{}, wrapStore(err)
	}
	e := waiterAsExecution(w)
	if err := s.holderMatches(ctx, e, sessionID); err != nil {
		return lease.Execution{}, err
	}
	if _, err := tx.Exec(ctx, `delete from waiters where waiter_id = $1`, waiterID); err != nil {
		return lease.Execution{}, wrapStore(err)
	}
	if err := insertAudit(ctx, tx, merchantID, auditRow{
		typ: "EXECUTION_DEQUEUED", customerID: w.CustomerID,
		principalType: w.Principal.Type, principalID: w.Principal.ID,
		domainKey: domainKey, executionID: waiterID, ruleName: w.RuleName,
		reason: "left_queue", requestID: requestID,
	}); err != nil {
		return lease.Execution{}, wrapStore(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return lease.Execution{}, wrapStore(err)
	}
	now := time.Now().UTC()
	e.State = lease.StateReleased
	e.EndReason = lease.ReasonReleased
	e.EndedAt = &now
	return e, nil
}

func (s *Store) ExpireWaiters(ctx context.Context, limit int) (int, error) {
	if limit < 1 {
		limit = 100
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, wrapStore(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		select waiter_id, merchant_id, customer_id, principal_type, principal_id, domain_key, rule_name
		from waiters
		where expires_at <= now()
		limit $1
		for update skip locked`, limit)
	if err != nil {
		return 0, wrapStore(err)
	}
	type expired struct {
		id, merchant, customer, ptype, pid, domain, rule string
	}
	var list []expired
	for rows.Next() {
		var d expired
		if err := rows.Scan(&d.id, &d.merchant, &d.customer, &d.ptype, &d.pid, &d.domain, &d.rule); err != nil {
			rows.Close()
			return 0, wrapStore(err)
		}
		list = append(list, d)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, wrapStore(err)
	}
	for _, d := range list {
		if _, err := tx.Exec(ctx, `delete from waiters where waiter_id = $1`, d.id); err != nil {
			return 0, wrapStore(err)
		}
		if err := insertAudit(ctx, tx, d.merchant, auditRow{
			typ: "EXECUTION_QUEUE_EXPIRED", customerID: d.customer,
			principalType: d.ptype, principalID: d.pid,
			domainKey: d.domain, executionID: d.id, ruleName: d.rule,
			reason: "expired",
		}); err != nil {
			return 0, wrapStore(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, wrapStore(err)
	}
	return len(list), nil
}

func (s *Store) ListWaiters(ctx context.Context, merchantID, customerID string, limit int) ([]lease.Execution, error) {
	if limit < 1 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, waiterSelect+`
		where merchant_id = $1 and expires_at > now()
		  and ($2 = '' or customer_id = $2)
		order by created_at
		limit $3`, merchantID, customerID, limit)
	if err != nil {
		return nil, wrapStore(err)
	}
	defer rows.Close()
	var out []lease.Execution
	for rows.Next() {
		w, err := scanWaiter(rows)
		if err != nil {
			return nil, wrapStore(err)
		}
		out = append(out, waiterAsExecution(w))
	}
	return out, rows.Err()
}

func (s *Store) CountWaiters(ctx context.Context, merchantID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		select count(*) from waiters
		where merchant_id = $1 and expires_at > now()`, merchantID).Scan(&n)
	return n, wrapStore(err)
}
