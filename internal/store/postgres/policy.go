package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Nareik33L/bruiser-gateway/internal/lease"
	"github.com/Nareik33L/bruiser-gateway/internal/policy"
)

type PolicyRow struct {
	MerchantID string
	Version    int
	YAML       string
	Active     bool
}

func (s *Store) PutPolicy(ctx context.Context, merchantID, yamlText, requestID string) (PolicyRow, error) {
	if merchantID == "" || yamlText == "" {
		return PolicyRow{}, lease.ErrInvalidInput
	}
	if _, err := policy.CompileYAML([]byte(yamlText)); err != nil {
		return PolicyRow{}, fmt.Errorf("%w: %v", lease.ErrInvalidInput, err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return PolicyRow{}, wrapStore(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var next int
	if err := tx.QueryRow(ctx, `
		select coalesce(max(version), 0) + 1 from policies where merchant_id = $1`, merchantID,
	).Scan(&next); err != nil {
		return PolicyRow{}, wrapStore(err)
	}
	if _, err := tx.Exec(ctx, `
		update policies set active = false where merchant_id = $1 and active`, merchantID); err != nil {
		return PolicyRow{}, wrapStore(err)
	}
	if _, err := tx.Exec(ctx, `
		insert into policies (merchant_id, version, yaml, active)
		values ($1, $2, $3, true)`, merchantID, next, yamlText); err != nil {
		return PolicyRow{}, wrapStore(err)
	}
	if err := insertAudit(ctx, tx, merchantID, auditRow{
		typ: "POLICY_UPDATED", reason: "put", requestID: requestID,
	}); err != nil {
		return PolicyRow{}, wrapStore(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return PolicyRow{}, wrapStore(err)
	}
	_, _ = s.pool.Exec(ctx, `select pg_notify('bruiser_policy', $1)`, merchantID)
	return PolicyRow{MerchantID: merchantID, Version: next, YAML: yamlText, Active: true}, nil
}

func (s *Store) ActivePolicy(ctx context.Context, merchantID string) (PolicyRow, error) {
	var row PolicyRow
	err := s.pool.QueryRow(ctx, `
		select merchant_id, version, yaml, active from policies
		where merchant_id = $1 and active`, merchantID,
	).Scan(&row.MerchantID, &row.Version, &row.YAML, &row.Active)
	if errors.Is(err, pgx.ErrNoRows) {
		return PolicyRow{}, lease.ErrNotFound
	}
	if err != nil {
		return PolicyRow{}, wrapStore(err)
	}
	return row, nil
}

func (s *Store) PolicyHistory(ctx context.Context, merchantID string, limit int) ([]PolicyRow, error) {
	if limit < 1 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `
		select merchant_id, version, yaml, active from policies
		where merchant_id = $1 order by version desc limit $2`, merchantID, limit)
	if err != nil {
		return nil, wrapStore(err)
	}
	defer rows.Close()
	var out []PolicyRow
	for rows.Next() {
		var r PolicyRow
		if err := rows.Scan(&r.MerchantID, &r.Version, &r.YAML, &r.Active); err != nil {
			return nil, wrapStore(err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
