package postgres

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Nareik33L/bruiser-gateway/internal/lease"
)

// AuditEvent is a read model of one audit_events row for operator views.
type AuditEvent struct {
	Seq           int64          `json:"seq"`
	At            time.Time      `json:"at"`
	Type          string         `json:"type"`
	CustomerID    string         `json:"customer_id,omitempty"`
	PrincipalType string         `json:"principal_type,omitempty"`
	PrincipalID   string         `json:"principal_id,omitempty"`
	DomainKey     string         `json:"domain_key,omitempty"`
	ExecutionID   string         `json:"execution_id,omitempty"`
	Fence         *int64         `json:"fence,omitempty"`
	RuleName      string         `json:"rule_name,omitempty"`
	Reason        string         `json:"reason,omitempty"`
	RequestID     string         `json:"request_id,omitempty"`
	Attrs         map[string]any `json:"attrs,omitempty"`
}

// ListActive returns live executions for a merchant, newest grant first.
func (s *Store) ListActive(ctx context.Context, merchantID string, limit int) ([]lease.Execution, error) {
	if limit < 1 {
		limit = 500
	}
	rows, err := s.pool.Query(ctx, `
		select execution_id, merchant_id, domain_key, customer_id, principal_type, principal_id,
		       session_id, resource, action, rule_name, fence, state, granted_at, expires_at,
		       max_lifetime_at, renew_count, ended_at, end_reason, successor_id
		from executions
		where merchant_id = $1 and state = 'ACTIVE' and expires_at > now()
		order by granted_at desc
		limit $2`, merchantID, limit)
	if err != nil {
		return nil, wrapStore(err)
	}
	out, err := scanExecutions(rows)
	if err != nil {
		return nil, wrapStore(err)
	}
	return out, nil
}

// CountActive counts live executions for a merchant.
func (s *Store) CountActive(ctx context.Context, merchantID string) (int, error) {
	var n int
	err := s.pool.QueryRow(ctx, `
		select count(*) from executions
		where merchant_id = $1 and state = 'ACTIVE' and expires_at > now()`, merchantID).Scan(&n)
	return n, wrapStore(err)
}

// ListAudit returns the most recent audit events for a merchant.
func (s *Store) ListAudit(ctx context.Context, merchantID string, limit int) ([]AuditEvent, error) {
	if limit < 1 || limit > 1000 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		select seq, at, type, coalesce(customer_id,''), coalesce(principal_type,''),
		       coalesce(principal_id,''), coalesce(domain_key,''), coalesce(execution_id,''),
		       fence, coalesce(rule_name,''), coalesce(reason,''), coalesce(request_id,''), attrs
		from audit_events
		where merchant_id = $1
		order by seq desc
		limit $2`, merchantID, limit)
	if err != nil {
		return nil, wrapStore(err)
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var a AuditEvent
		var attrs []byte
		if err := rows.Scan(&a.Seq, &a.At, &a.Type, &a.CustomerID, &a.PrincipalType, &a.PrincipalID,
			&a.DomainKey, &a.ExecutionID, &a.Fence, &a.RuleName, &a.Reason, &a.RequestID, &attrs); err != nil {
			return nil, wrapStore(err)
		}
		if len(attrs) > 2 {
			_ = json.Unmarshal(attrs, &a.Attrs)
		}
		out = append(out, a)
	}
	return out, wrapStore(rows.Err())
}
