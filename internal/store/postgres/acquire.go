package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Nareik33L/bruiser-gateway/internal/id"
	"github.com/Nareik33L/bruiser-gateway/internal/lease"
)

func (s *Store) Acquire(ctx context.Context, req lease.AcquireRequest) (lease.AcquireResult, error) {
	if req.MerchantID == "" || req.DomainKey == "" || req.CustomerID == "" || req.Principal.ID == "" {
		return lease.AcquireResult{}, lease.ErrInvalidInput
	}
	if req.MaxActive < 1 {
		req.MaxActive = 1
	}
	if req.TTL <= 0 {
		req.TTL = 60 * time.Second
	}
	if req.MaxLifetime <= 0 {
		req.MaxLifetime = 15 * time.Minute
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return lease.AcquireResult{}, wrapStore(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		insert into domains (merchant_id, domain_key) values ($1, $2)
		on conflict do nothing`, req.MerchantID, req.DomainKey); err != nil {
		return lease.AcquireResult{}, wrapStore(err)
	}

	var fence int64
	if err := tx.QueryRow(ctx, `
		select fence from domains
		where merchant_id = $1 and domain_key = $2
		for update`, req.MerchantID, req.DomainKey).Scan(&fence); err != nil {
		return lease.AcquireResult{}, wrapStore(err)
	}

	rows, err := tx.Query(ctx, `
		select execution_id, merchant_id, domain_key, customer_id, principal_type, principal_id,
		       session_id, resource, action, rule_name, fence, state, granted_at, expires_at,
		       max_lifetime_at, renew_count, ended_at, end_reason, successor_id
		from executions
		where merchant_id = $1 and domain_key = $2 and state = 'ACTIVE' and expires_at > now()`,
		req.MerchantID, req.DomainKey)
	if err != nil {
		return lease.AcquireResult{}, wrapStore(err)
	}
	actives, err := scanExecutions(rows)
	if err != nil {
		return lease.AcquireResult{}, wrapStore(err)
	}

	for i := range actives {
		a := actives[i]
		if a.Principal.Type == req.Principal.Type && a.Principal.ID == req.Principal.ID {
			if err := insertAudit(ctx, tx, req.MerchantID, auditRow{
				typ: "EXECUTION_REQUESTED", customerID: req.CustomerID,
				principalType: req.Principal.Type, principalID: req.Principal.ID,
				domainKey: req.DomainKey, executionID: a.ID, ruleName: req.RuleName,
				reason: "already_held", requestID: req.RequestID, fence: &a.Fence,
			}); err != nil {
				return lease.AcquireResult{}, wrapStore(err)
			}
			if err := tx.Commit(ctx); err != nil {
				return lease.AcquireResult{}, wrapStore(err)
			}
			return lease.AcquireResult{Status: lease.StatusAlreadyHeld, Execution: &a}, nil
		}
	}

	if len(actives) >= req.MaxActive {
		holder := actives[0]
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
			},
		}, nil
	}

	if err := tx.QueryRow(ctx, `
		update domains set fence = fence + 1, updated_at = now()
		where merchant_id = $1 and domain_key = $2
		returning fence`, req.MerchantID, req.DomainKey).Scan(&fence); err != nil {
		return lease.AcquireResult{}, wrapStore(err)
	}

	exe := lease.Execution{
		ID:            id.Execution(),
		MerchantID:    req.MerchantID,
		DomainKey:     req.DomainKey,
		CustomerID:    req.CustomerID,
		Principal:     req.Principal,
		SessionID:     req.SessionID,
		Resource:      req.Resource,
		Action:        req.Action,
		RuleName:      req.RuleName,
		Fence:         fence,
		State:         lease.StateActive,
		RenewCount:    0,
	}

	if err := tx.QueryRow(ctx, `
		insert into executions (
			execution_id, merchant_id, domain_key, customer_id, principal_type, principal_id,
			session_id, resource, action, rule_name, fence, state,
			granted_at, expires_at, max_lifetime_at, renew_count
		) values (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'ACTIVE',
			now(), now() + $12::interval, now() + $13::interval, 0
		)
		returning granted_at, expires_at, max_lifetime_at`,
		exe.ID, req.MerchantID, req.DomainKey, req.CustomerID,
		req.Principal.Type, req.Principal.ID, req.SessionID,
		req.Resource, req.Action, req.RuleName, fence,
		interval(req.TTL), interval(req.MaxLifetime),
	).Scan(&exe.GrantedAt, &exe.ExpiresAt, &exe.MaxLifetimeAt); err != nil {
		return lease.AcquireResult{}, wrapStore(err)
	}

	if err := insertAudit(ctx, tx, req.MerchantID, auditRow{
		typ: "EXECUTION_GRANTED", customerID: req.CustomerID,
		principalType: req.Principal.Type, principalID: req.Principal.ID,
		domainKey: req.DomainKey, executionID: exe.ID, ruleName: req.RuleName,
		reason: "granted", requestID: req.RequestID, fence: &fence,
	}); err != nil {
		return lease.AcquireResult{}, wrapStore(err)
	}

	if err := tx.Commit(ctx); err != nil {
		return lease.AcquireResult{}, wrapStore(err)
	}
	return lease.AcquireResult{Status: lease.StatusGranted, Execution: &exe}, nil
}

func interval(d time.Duration) string {
	return fmt.Sprintf("%d milliseconds", d.Milliseconds())
}

func scanExecutions(rows pgx.Rows) ([]lease.Execution, error) {
	defer rows.Close()
	var out []lease.Execution
	for rows.Next() {
		e, err := scanExecution(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanExecution(row rowScanner) (lease.Execution, error) {
	var e lease.Execution
	var endedAt *time.Time
	var endReason, successor *string
	err := row.Scan(
		&e.ID, &e.MerchantID, &e.DomainKey, &e.CustomerID, &e.Principal.Type, &e.Principal.ID,
		&e.SessionID, &e.Resource, &e.Action, &e.RuleName, &e.Fence, &e.State,
		&e.GrantedAt, &e.ExpiresAt, &e.MaxLifetimeAt, &e.RenewCount,
		&endedAt, &endReason, &successor,
	)
	if err != nil {
		return e, err
	}
	e.EndedAt = endedAt
	if endReason != nil {
		e.EndReason = *endReason
	}
	if successor != nil {
		e.SuccessorID = *successor
	}
	return e, nil
}

func (s *Store) Get(ctx context.Context, merchantID, executionID string) (lease.Execution, error) {
	row := s.pool.QueryRow(ctx, `
		select execution_id, merchant_id, domain_key, customer_id, principal_type, principal_id,
		       session_id, resource, action, rule_name, fence, state, granted_at, expires_at,
		       max_lifetime_at, renew_count, ended_at, end_reason, successor_id
		from executions
		where merchant_id = $1 and execution_id = $2`, merchantID, executionID)
	e, err := scanExecution(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return lease.Execution{}, lease.ErrNotFound
	}
	if err != nil {
		return lease.Execution{}, wrapStore(err)
	}
	return e, nil
}
