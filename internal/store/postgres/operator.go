package postgres

import (
	"context"
	"encoding/json"
)

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
