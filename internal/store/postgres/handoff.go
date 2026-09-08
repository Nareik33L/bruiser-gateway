package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Nareik33L/bruiser-gateway/internal/id"
	"github.com/Nareik33L/bruiser-gateway/internal/lease"
)

func (s *Store) Handoff(ctx context.Context, req lease.HandoffRequest) (lease.HandoffResult, error) {
	if req.MerchantID == "" || req.ExecutionID == "" || req.SessionID == "" {
		return lease.HandoffResult{}, lease.ErrInvalidInput
	}
	if req.TTL <= 0 {
		req.TTL = 60 * time.Second
	}

	caller, err := s.Session(ctx, req.SessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return lease.HandoffResult{}, lease.ErrNotHolder
		}
		return lease.HandoffResult{}, err
	}
	callerP := lease.Principal{Type: caller.PrincipalType, ID: caller.PrincipalID}

	domainKey, err := s.lookupDomainKey(ctx, req.MerchantID, req.ExecutionID)
	if err != nil {
		return lease.HandoffResult{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return lease.HandoffResult{}, wrapStore(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.lockDomain(ctx, tx, req.MerchantID, domainKey); err != nil {
		return lease.HandoffResult{}, wrapStore(err)
	}
	old, err := s.loadActiveForUpdate(ctx, tx, req.MerchantID, req.ExecutionID)
	if err != nil {
		return lease.HandoffResult{}, wrapStore(err)
	}
	if old.CustomerID != caller.CustomerID {
		return lease.HandoffResult{}, lease.ErrNotFound
	}

	mode := req.Mode
	to := req.To
	isHolder := s.holderMatches(ctx, old, req.SessionID) == nil

	switch mode {
	case lease.ModeCooperative:
		if !isHolder {
			return lease.HandoffResult{}, lease.ErrNotHolder
		}
		if to.Type == "" || to.ID == "" {
			return lease.HandoffResult{}, lease.ErrInvalidInput
		}
	case lease.ModePreempt, "":
		if isHolder {
			mode = lease.ModeCooperative
			if to.Type == "" || to.ID == "" {
				return lease.HandoffResult{}, lease.ErrInvalidInput
			}
		} else if lease.CanPreemptRanked(callerP, old.Principal, req.Precedence) {
			mode = lease.ModePreempt
			if to.Type == "" {
				to = callerP
			}
			if !lease.SamePrincipal(to, callerP) {
				return lease.HandoffResult{}, lease.ErrPrecedence
			}
		} else {
			return lease.HandoffResult{}, lease.ErrPrecedence
		}
	default:
		return lease.HandoffResult{}, lease.ErrInvalidInput
	}

	if lease.SamePrincipal(to, old.Principal) {
		return lease.HandoffResult{}, lease.ErrInvalidInput
	}

	var fence int64
	if err := tx.QueryRow(ctx, `
		update domains set fence = fence + 1, updated_at = now()
		where merchant_id = $1 and domain_key = $2
		returning fence`, old.MerchantID, old.DomainKey).Scan(&fence); err != nil {
		return lease.HandoffResult{}, wrapStore(err)
	}

	succID := id.Execution()
	succSession := req.SessionID
	if mode == lease.ModeCooperative {
		succSession = id.Session()
	}

	succ := lease.Execution{
		ID:         succID,
		MerchantID: old.MerchantID,
		DomainKey:  old.DomainKey,
		CustomerID: old.CustomerID,
		Principal:  to,
		SessionID:  succSession,
		Resource:   old.Resource,
		Action:     old.Action,
		RuleName:   old.RuleName,
		Fence:      fence,
		State:      lease.StateActive,
	}

	if err := tx.QueryRow(ctx, `
		insert into executions (
			execution_id, merchant_id, domain_key, customer_id, principal_type, principal_id,
			session_id, resource, action, rule_name, fence, state,
			granted_at, expires_at, max_lifetime_at, renew_count
		) values (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'ACTIVE',
			now(), least(now() + $12::interval, $13::timestamptz), $13, 0
		)
		returning granted_at, expires_at, max_lifetime_at`,
		succ.ID, old.MerchantID, old.DomainKey, old.CustomerID,
		to.Type, to.ID, succSession,
		old.Resource, old.Action, old.RuleName, fence,
		interval(req.TTL), old.MaxLifetimeAt,
	).Scan(&succ.GrantedAt, &succ.ExpiresAt, &succ.MaxLifetimeAt); err != nil {
		return lease.HandoffResult{}, wrapStore(err)
	}

	var endedAt time.Time
	if err := tx.QueryRow(ctx, `
		update executions
		set state = 'HANDED_OFF', ended_at = now(), end_reason = 'HANDED_OFF', successor_id = $2
		where execution_id = $1
		returning ended_at`, old.ID, succ.ID,
	).Scan(&endedAt); err != nil {
		return lease.HandoffResult{}, wrapStore(err)
	}
	old.State = lease.StateHandedOff
	old.EndReason = lease.ReasonHandedOff
	old.SuccessorID = succ.ID
	old.EndedAt = &endedAt

	if err := insertAudit(ctx, tx, req.MerchantID, auditRow{
		typ: "EXECUTION_HANDED_OFF", customerID: old.CustomerID,
		principalType: old.Principal.Type, principalID: old.Principal.ID,
		domainKey: old.DomainKey, executionID: old.ID, ruleName: old.RuleName,
		reason: mode, requestID: req.RequestID, fence: &old.Fence,
	}); err != nil {
		return lease.HandoffResult{}, wrapStore(err)
	}
	if err := insertAudit(ctx, tx, req.MerchantID, auditRow{
		typ: "EXECUTION_GRANTED", customerID: succ.CustomerID,
		principalType: succ.Principal.Type, principalID: succ.Principal.ID,
		domainKey: succ.DomainKey, executionID: succ.ID, ruleName: succ.RuleName,
		reason: "handoff", requestID: req.RequestID, fence: &fence,
	}); err != nil {
		return lease.HandoffResult{}, wrapStore(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return lease.HandoffResult{}, wrapStore(err)
	}
	return lease.HandoffResult{Predecessor: old, Successor: succ, Mode: mode}, nil
}
