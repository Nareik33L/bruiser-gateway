package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Nareik33L/bruiser-gateway/internal/lease"
)

type AuditEvent struct {
	Seq           int64          `json:"seq"`
	MerchantID    string         `json:"merchant_id"`
	At            time.Time      `json:"at"`
	Type          string         `json:"type"`
	CustomerID    string         `json:"customer_id"`
	PrincipalType string         `json:"principal_type"`
	PrincipalID   string         `json:"principal_id"`
	DomainKey     string         `json:"domain_key"`
	ExecutionID   string         `json:"execution_id"`
	Fence         *int64         `json:"fence,omitempty"`
	RuleName      string         `json:"rule_name"`
	Reason        string         `json:"reason"`
	RequestID     string         `json:"request_id"`
	Attrs         map[string]any `json:"attrs"`
}

type UsageFigures struct {
	ActiveExecutions    int `json:"active_executions"`
	Executions30d       int `json:"executions_30d"`
	ResourcesProtected  int `json:"resources_protected"`
	PeakConcurrentToday int `json:"peak_concurrent_today"`
	QueueDepth          int `json:"queue_depth"`
}

func (s *Store) WriteAudit(ctx context.Context, merchantID, typ, reason, requestID string, attrs map[string]any) error {
	return s.WriteActorAudit(ctx, merchantID, typ, "", "", reason, requestID, attrs)
}

func (s *Store) writeAdminAction(ctx context.Context, merchantID, typ, reason, requestID string, audit AdminAudit, attrs map[string]any) error {
	if attrs == nil {
		attrs = map[string]any{}
	}
	if audit.Actor != "" {
		attrs["actor"] = audit.Actor
	}
	if audit.Role != "" {
		attrs["role"] = audit.Role
	}
	if audit.Auth != "" {
		attrs["auth"] = audit.Auth
	}
	if audit.IP != "" {
		attrs["ip"] = audit.IP
	}
	if requestID == "" {
		requestID = audit.RequestID
	}
	return s.WriteActorAudit(ctx, merchantID, typ, audit.Role, audit.Actor, reason, requestID, attrs)
}

func (s *Store) WriteActorAudit(ctx context.Context, merchantID, typ, role, actor, reason, requestID string, attrs map[string]any) error {
	if merchantID == "" || typ == "" {
		return lease.ErrInvalidInput
	}
	raw, err := json.Marshal(attrs)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		insert into audit_events (merchant_id, type, principal_type, principal_id, reason, request_id, attrs)
		values ($1, $2, $3, $4, $5, $6, $7)`,
		merchantID, typ, nullIfEmpty(role), nullIfEmpty(actor), nullIfEmpty(reason), nullIfEmpty(requestID), attrsOrEmpty(raw),
	)
	return wrapStore(err)
}

func (s *Store) LastAudit(ctx context.Context, merchantID, typ string) (AuditEvent, error) {
	var ev AuditEvent
	var attrs []byte
	err := s.pool.QueryRow(ctx, `
		select seq, merchant_id, at, type, coalesce(customer_id,''), coalesce(principal_type,''),
		       coalesce(principal_id,''), coalesce(domain_key,''), coalesce(execution_id,''),
		       fence, coalesce(rule_name,''), coalesce(reason,''), coalesce(request_id,''), attrs
		from audit_events
		where merchant_id = $1 and type = $2
		order by at desc, seq desc
		limit 1`, merchantID, typ,
	).Scan(&ev.Seq, &ev.MerchantID, &ev.At, &ev.Type, &ev.CustomerID, &ev.PrincipalType,
		&ev.PrincipalID, &ev.DomainKey, &ev.ExecutionID, &ev.Fence, &ev.RuleName, &ev.Reason,
		&ev.RequestID, &attrs)
	if errors.Is(err, pgx.ErrNoRows) {
		return ev, lease.ErrNotFound
	}
	if err != nil {
		return ev, wrapStore(err)
	}
	_ = json.Unmarshal(attrs, &ev.Attrs)
	if ev.Attrs == nil {
		ev.Attrs = map[string]any{}
	}
	return ev, nil
}

func (s *Store) ExportAudit(ctx context.Context, merchantID string, since time.Time, limit int) ([]AuditEvent, error) {
	if limit < 1 {
		limit = 1000
	}
	if since.IsZero() {
		since = time.Now().UTC().Add(-24 * time.Hour)
	}
	rows, err := s.pool.Query(ctx, `
		select seq, merchant_id, at, type, coalesce(customer_id,''), coalesce(principal_type,''),
		       coalesce(principal_id,''), coalesce(domain_key,''), coalesce(execution_id,''),
		       fence, coalesce(rule_name,''), coalesce(reason,''), coalesce(request_id,''), attrs
		from audit_events
		where merchant_id = $1 and at >= $2
		order by seq
		limit $3`, merchantID, since, limit)
	if err != nil {
		return nil, wrapStore(err)
	}
	defer rows.Close()
	return scanAudit(rows)
}

func (s *Store) SearchAudit(ctx context.Context, merchantID, customerID, typ string, limit int) ([]AuditEvent, error) {
	if limit < 1 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		select seq, merchant_id, at, type, coalesce(customer_id,''), coalesce(principal_type,''),
		       coalesce(principal_id,''), coalesce(domain_key,''), coalesce(execution_id,''),
		       fence, coalesce(rule_name,''), coalesce(reason,''), coalesce(request_id,''), attrs
		from audit_events
		where merchant_id = $1
		  and ($2 = '' or customer_id = $2)
		  and ($3 = '' or type = $3)
		order by at desc, seq desc
		limit $4`, merchantID, customerID, typ, limit)
	if err != nil {
		return nil, wrapStore(err)
	}
	defer rows.Close()
	return scanAudit(rows)
}

func scanAudit(rows pgx.Rows) ([]AuditEvent, error) {
	var out []AuditEvent
	for rows.Next() {
		var ev AuditEvent
		var attrs []byte
		if err := rows.Scan(&ev.Seq, &ev.MerchantID, &ev.At, &ev.Type, &ev.CustomerID, &ev.PrincipalType,
			&ev.PrincipalID, &ev.DomainKey, &ev.ExecutionID, &ev.Fence, &ev.RuleName, &ev.Reason,
			&ev.RequestID, &attrs); err != nil {
			return nil, wrapStore(err)
		}
		_ = json.Unmarshal(attrs, &ev.Attrs)
		if ev.Attrs == nil {
			ev.Attrs = map[string]any{}
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (s *Store) PurgeAudit(ctx context.Context, merchantID string, olderThan time.Time) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, wrapStore(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `
		delete from audit_events
		where merchant_id = $1 and at < $2 and type <> 'AUDIT_PURGED'`,
		merchantID, olderThan)
	if err != nil {
		return 0, wrapStore(err)
	}
	n := tag.RowsAffected()
	if n > 0 {
		attrs, _ := json.Marshal(map[string]any{"purged": n, "older_than": olderThan.UTC().Format(time.RFC3339)})
		if err := insertAudit(ctx, tx, merchantID, auditRow{
			typ: "AUDIT_PURGED", reason: "retention", attrs: attrs,
		}); err != nil {
			return 0, wrapStore(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, wrapStore(err)
	}
	return n, nil
}

func (s *Store) UsageFigures(ctx context.Context, merchantID string) (UsageFigures, error) {
	var u UsageFigures
	err := s.pool.QueryRow(ctx, `
		select
			(select count(*) from executions
			  where merchant_id = $1 and state = 'ACTIVE' and expires_at > now()),
			(select count(*) from executions
			  where merchant_id = $1 and granted_at > now() - interval '30 days'),
			(select count(distinct resource) from executions where merchant_id = $1),
			(select coalesce(max(c), 0) from (
			    select count(*) as c from executions
			    where merchant_id = $1 and granted_at::date = current_date
			    group by date_trunc('minute', granted_at)
			) s),
			(select count(*) from waiters
			  where merchant_id = $1 and expires_at > now())`,
		merchantID,
	).Scan(&u.ActiveExecutions, &u.Executions30d, &u.ResourcesProtected, &u.PeakConcurrentToday, &u.QueueDepth)
	return u, wrapStore(err)
}

func (s *Store) ListActive(ctx context.Context, merchantID, customerID string, limit int) ([]lease.Execution, error) {
	if limit < 1 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		select execution_id, merchant_id, domain_key, customer_id, principal_type, principal_id,
		       session_id, resource, action, rule_name, fence, state, granted_at, expires_at,
		       max_lifetime_at, renew_count, coalesce(end_reason,''), coalesce(successor_id,'')
		from executions
		where merchant_id = $1 and state = 'ACTIVE' and expires_at > now()
		  and ($2 = '' or customer_id = $2)
		order by granted_at desc
		limit $3`, merchantID, customerID, limit)
	if err != nil {
		return nil, wrapStore(err)
	}
	defer rows.Close()
	var out []lease.Execution
	for rows.Next() {
		var e lease.Execution
		if err := rows.Scan(&e.ID, &e.MerchantID, &e.DomainKey, &e.CustomerID, &e.Principal.Type, &e.Principal.ID,
			&e.SessionID, &e.Resource, &e.Action, &e.RuleName, &e.Fence, &e.State, &e.GrantedAt, &e.ExpiresAt,
			&e.MaxLifetimeAt, &e.RenewCount, &e.EndReason, &e.SuccessorID); err != nil {
			return nil, wrapStore(err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
