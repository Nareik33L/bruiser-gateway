package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Nareik33L/bruiser-gateway/internal/id"
	"github.com/Nareik33L/bruiser-gateway/internal/ops"
)

func (s *Store) GetControls(ctx context.Context, merchantID string) (ops.Controls, bool, error) {
	c := ops.Default()
	var disabled, scope string
	err := s.pool.QueryRow(ctx, `
		select mode, enforcement, queue_enabled, max_waiters, lease_ttl_seconds,
		       fail_closed, disabled_actions, coalesce(updated_by,''),
		       enforce_percent, coalesce(ramp_salt,''), coalesce(ramp_scope,'{}')
		from runtime_controls where merchant_id = $1`, merchantID,
	).Scan(&c.Mode, &c.Enforcement, &c.QueueEnabled, &c.MaxWaiters, &c.LeaseTTLSeconds,
		&c.FailClosed, &disabled, &c.UpdatedBy, &c.EnforcePercent, &c.RampSalt, &scope)
	if errors.Is(err, pgx.ErrNoRows) {
		return ops.Default(), false, nil
	}
	if err != nil {
		return c, false, wrapStore(err)
	}
	c.DisabledActions = ops.SplitActions(disabled)
	if scope != "" && scope != "{}" {
		_ = json.Unmarshal([]byte(scope), &c.Scope)
	}
	return ops.Normalize(c), true, nil
}

func (s *Store) PutControls(ctx context.Context, merchantID string, c ops.Controls, audit AdminAudit) (ops.Controls, error) {
	c = ops.Normalize(c)
	scope, _ := json.Marshal(c.Scope)
	_, err := s.pool.Exec(ctx, `
		insert into runtime_controls (
			merchant_id, mode, enforcement, queue_enabled, max_waiters,
			lease_ttl_seconds, fail_closed, disabled_actions, updated_at, updated_by,
			enforce_percent, ramp_salt, ramp_scope
		) values ($1,$2,$3,$4,$5,$6,$7,$8,now(),$9,$10,$11,$12)
		on conflict (merchant_id) do update set
		  mode = excluded.mode,
		  enforcement = excluded.enforcement,
		  queue_enabled = excluded.queue_enabled,
		  max_waiters = excluded.max_waiters,
		  lease_ttl_seconds = excluded.lease_ttl_seconds,
		  fail_closed = excluded.fail_closed,
		  disabled_actions = excluded.disabled_actions,
		  updated_at = now(),
		  updated_by = excluded.updated_by,
		  enforce_percent = excluded.enforce_percent,
		  ramp_salt = excluded.ramp_salt,
		  ramp_scope = excluded.ramp_scope`,
		merchantID, c.Mode, c.Enforcement, c.QueueEnabled, c.MaxWaiters,
		c.LeaseTTLSeconds, c.FailClosed, ops.JoinActions(c.DisabledActions), c.UpdatedBy,
		c.EnforcePercent, c.RampSalt, string(scope))
	if err != nil {
		return c, wrapStore(err)
	}
	if err := s.writeAdminAction(ctx, merchantID, "CONTROL_CHANGED", c.Mode, id.Request(), audit, map[string]any{
		"enforcement":       c.Enforcement,
		"enforce_percent":   c.EnforcePercent,
		"effective_percent": c.EffectivePercent(),
		"ramp_salt":         c.RampSalt,
		"scope":             c.Scope,
		"queue_enabled":     c.QueueEnabled,
		"max_waiters":       c.MaxWaiters,
		"lease_ttl_seconds": c.LeaseTTLSeconds,
		"fail_closed":       c.FailClosed,
		"disabled_actions":  c.DisabledActions,
		"updated_by":        c.UpdatedBy,
	}); err != nil {
		return c, err
	}
	return c, nil
}

func (s *Store) DrainWaiters(ctx context.Context, merchantID, requestID string, audit AdminAudit) (int, error) {
	tag, err := s.pool.Exec(ctx, `delete from waiters where merchant_id = $1`, merchantID)
	if err != nil {
		return 0, wrapStore(err)
	}
	n := int(tag.RowsAffected())
	if err := s.writeAdminAction(ctx, merchantID, "EMERGENCY_DRAIN", "waiters", requestID, audit, map[string]any{"count": n}); err != nil {
		return n, err
	}
	return n, nil
}

func (s *Store) RevokeActive(ctx context.Context, merchantID, requestID, reason string, audit AdminAudit) (int, error) {
	if reason == "" {
		reason = "emergency"
	}
	execs, err := s.ListActive(ctx, merchantID, "", 10000)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range execs {
		if _, err := s.Revoke(ctx, merchantID, e.ID, "", requestID, reason); err == nil {
			n++
		}
	}
	if err := s.writeAdminAction(ctx, merchantID, "EMERGENCY_REVOKE", reason, requestID, audit, map[string]any{"count": n}); err != nil {
		return n, err
	}
	return n, nil
}

type DryRunEvent struct {
	Would       string
	Reason      string
	CustomerID  string
	PrincipalID string
	Resource    string
	Action      string
	RuleName    string
	RequestID   string
	Enforced    bool
	Error       string
}

func (s *Store) ShadowDecide(ctx context.Context, merchantID, domainKey, customerID, principalID, resource, action, ruleName string, maxActive, maxWaiters int, ttl time.Duration, requestID string) (DryRunEvent, error) {
	if maxActive < 1 {
		maxActive = 1
	}
	if ttl <= 0 {
		ttl = time.Minute
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return DryRunEvent{}, wrapStore(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		insert into dry_run_events (merchant_id, customer_id, principal_id, resource, action, would, reason, rule_name, request_id)
		select merchant_id, customer_id, principal_id, resource, action, 'EXPIRE', 'stale_queued_execution', rule_name, $2
		from dry_run_holds
		where merchant_id = $1 and kind = 'QUEUED' and expires_at <= now()`,
		merchantID, requestID); err != nil {
		return DryRunEvent{}, wrapStore(err)
	}
	if _, err := tx.Exec(ctx, `
		delete from dry_run_holds where merchant_id = $1 and expires_at <= now()`, merchantID); err != nil {
		return DryRunEvent{}, wrapStore(err)
	}

	var kind string
	err = tx.QueryRow(ctx, `
		select kind from dry_run_holds
		where merchant_id = $1 and domain_key = $2 and principal_id = $3 and expires_at > now()
		limit 1`, merchantID, domainKey, principalID).Scan(&kind)
	if err == nil {
		ev := DryRunEvent{Would: "ALLOW", Reason: "already_held", CustomerID: customerID, PrincipalID: principalID, Resource: resource, Action: action, RuleName: ruleName, RequestID: requestID}
		return s.commitDry(ctx, tx, merchantID, ev)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return DryRunEvent{}, wrapStore(err)
	}

	var active, queued int
	if err := tx.QueryRow(ctx, `
		select
		  count(*) filter (where kind = 'ACTIVE'),
		  count(*) filter (where kind = 'QUEUED')
		from dry_run_holds
		where merchant_id = $1 and domain_key = $2 and expires_at > now()`,
		merchantID, domainKey).Scan(&active, &queued); err != nil {
		return DryRunEvent{}, wrapStore(err)
	}

	ev := DryRunEvent{CustomerID: customerID, PrincipalID: principalID, Resource: resource, Action: action, RuleName: ruleName, RequestID: requestID}
	holdKind := ""
	switch {
	case active < maxActive:
		ev.Would, ev.Reason, holdKind = "ALLOW", "would_acquire", "ACTIVE"
	case maxWaiters > 0 && queued < maxWaiters:
		ev.Would, ev.Reason, holdKind = "QUEUE", "active_execution_already_exists", "QUEUED"
	default:
		ev.Would, ev.Reason = "REJECT", "queue_capacity_exceeded"
	}
	if holdKind != "" {
		if _, err := tx.Exec(ctx, `
			insert into dry_run_holds (
				hold_id, merchant_id, domain_key, customer_id, principal_id,
				resource, action, rule_name, kind, expires_at
			) values ($1,$2,$3,$4,$5,$6,$7,$8,$9, now() + $10::interval)`,
			id.Execution(), merchantID, domainKey, customerID, principalID,
			resource, action, ruleName, holdKind,
			interval(ttl),
		); err != nil {
			return DryRunEvent{}, wrapStore(err)
		}
	}
	return s.commitDry(ctx, tx, merchantID, ev)
}

func (s *Store) commitDry(ctx context.Context, tx pgx.Tx, merchantID string, ev DryRunEvent) (DryRunEvent, error) {
	if _, err := tx.Exec(ctx, `
		insert into dry_run_events (merchant_id, customer_id, principal_id, resource, action, would, reason, rule_name, request_id, enforced, error)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		merchantID, ev.CustomerID, ev.PrincipalID, ev.Resource, ev.Action, ev.Would, ev.Reason, ev.RuleName, ev.RequestID, ev.Enforced, nullIfEmpty(ev.Error),
	); err != nil {
		return ev, wrapStore(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return ev, wrapStore(err)
	}
	return ev, nil
}

func (s *Store) RecordDecision(ctx context.Context, merchantID string, ev DryRunEvent) error {
	_, err := s.pool.Exec(ctx, `
		insert into dry_run_events (merchant_id, customer_id, principal_id, resource, action, would, reason, rule_name, request_id, enforced, error)
		values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		merchantID, ev.CustomerID, ev.PrincipalID, ev.Resource, ev.Action, ev.Would, ev.Reason, ev.RuleName, ev.RequestID, ev.Enforced, nullIfEmpty(ev.Error))
	if err != nil {
		return wrapStore(err)
	}
	return nil
}

type DryRunReport struct {
	WindowHours         int              `json:"window_hours"`
	Observed            int              `json:"requests_observed"`
	Evaluated           int              `json:"requests_evaluated"`
	Enforced            int              `json:"traffic_enforced"`
	NotEnforced         int              `json:"traffic_not_enforced"`
	Customers           int              `json:"unique_customers"`
	Executions          int              `json:"executions_observed"`
	WouldAllow          int              `json:"would_allow"`
	WouldQueue          int              `json:"would_queue"`
	WouldReject         int              `json:"would_reject"`
	WouldExpire         int              `json:"would_expire"`
	ActualAllow         int              `json:"actual_allow"`
	ActualQueue         int              `json:"actual_queue"`
	ActualReject        int              `json:"actual_reject"`
	Allowed             int              `json:"requests_allowed"`
	Queued              int              `json:"requests_queued"`
	Rejected            int              `json:"requests_rejected"`
	Affected            int              `json:"customers_affected"`
	Absorbed            int              `json:"requests_absorbed"`
	AbsorbedWould       int              `json:"requests_potentially_absorbed"`
	OriginLoadReduction float64          `json:"estimated_origin_load_reduction"`
	EAF                 float64          `json:"execution_amplification_factor"`
	Errors              int              `json:"errors"`
	PolicyViolations    int              `json:"policy_violations"`
	Recent              []map[string]any `json:"recent"`
}

func (s *Store) DryRunReport(ctx context.Context, merchantID string, window time.Duration, recentLimit int) (DryRunReport, error) {
	if window <= 0 {
		window = 24 * time.Hour
	}
	if recentLimit < 1 {
		recentLimit = 20
	}
	var r DryRunReport
	r.WindowHours = int(window.Hours())
	err := s.pool.QueryRow(ctx, `
		select
		  count(*),
		  count(*) filter (where enforced),
		  count(*) filter (where not enforced),
		  count(distinct customer_id),
		  count(*) filter (where not enforced and would = 'ALLOW'),
		  count(*) filter (where not enforced and would = 'QUEUE'),
		  count(*) filter (where not enforced and would = 'REJECT'),
		  count(*) filter (where would = 'EXPIRE'),
		  count(*) filter (where enforced and would = 'ALLOW'),
		  count(*) filter (where enforced and would = 'QUEUE'),
		  count(*) filter (where enforced and would = 'REJECT'),
		  count(*) filter (where would in ('DENIED','ERROR') or error is not null),
		  count(*) filter (where would = 'DENIED'),
		  count(distinct customer_id) filter (where would in ('QUEUE','REJECT','EXPIRE'))
		from dry_run_events
		where merchant_id = $1 and at > now() - $2::interval`,
		merchantID, interval(window),
	).Scan(&r.Observed, &r.Enforced, &r.NotEnforced, &r.Customers,
		&r.WouldAllow, &r.WouldQueue, &r.WouldReject, &r.WouldExpire,
		&r.ActualAllow, &r.ActualQueue, &r.ActualReject,
		&r.Errors, &r.PolicyViolations, &r.Affected)
	if err != nil {
		return r, wrapStore(err)
	}
	r.Evaluated = r.Observed
	r.Allowed = r.WouldAllow + r.ActualAllow
	r.Queued = r.WouldQueue + r.ActualQueue
	r.Rejected = r.WouldReject + r.ActualReject
	r.Executions = r.WouldAllow + r.ActualAllow
	r.AbsorbedWould = r.WouldQueue + r.WouldReject
	r.Absorbed = r.WouldQueue + r.WouldReject + r.ActualQueue + r.ActualReject
	if r.Executions > 0 {
		r.EAF = float64(r.Observed) / float64(r.Executions)
	}
	if r.Observed > 0 {
		r.OriginLoadReduction = float64(r.Absorbed) / float64(r.Observed)
	}
	rows, err := s.pool.Query(ctx, `
		select at, coalesce(customer_id,''), coalesce(principal_id,''), coalesce(resource,''),
		       would, coalesce(reason,''), coalesce(rule_name,''), enforced
		from dry_run_events
		where merchant_id = $1 and at > now() - $2::interval
		order by at desc
		limit $3`, merchantID, interval(window), recentLimit)
	if err != nil {
		return r, wrapStore(err)
	}
	defer rows.Close()
	for rows.Next() {
		var at time.Time
		var customer, principal, resource, would, reason, rule string
		var enforced bool
		if err := rows.Scan(&at, &customer, &principal, &resource, &would, &reason, &rule, &enforced); err != nil {
			return r, wrapStore(err)
		}
		kind := "hypothetical"
		if enforced {
			kind = "enforced"
		}
		r.Recent = append(r.Recent, map[string]any{
			"at": at.UTC().Format(time.RFC3339Nano), "customer_id": customer,
			"principal_id": principal, "resource": resource,
			"would": would, "reason": reason, "rule_name": rule,
			"enforced": enforced, "kind": kind,
		})
	}
	return r, rows.Err()
}
