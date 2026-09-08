package postgres

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"

	"github.com/Nareik33L/bruiser-gateway/internal/lease"
)

//go:embed migrations/*.sql
var embedMigrations embed.FS

type Store struct {
	pool *pgxpool.Pool
}

func Connect(ctx context.Context, url string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 8
	cfg.MinConns = 0
	cfg.MaxConnIdleTime = 5 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("%w: %v", lease.ErrUnavailable, err)
	}
	return &Store{pool: pool}, nil
}

var migrateMu sync.Mutex

func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

func (s *Store) Ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("%w: %v", lease.ErrUnavailable, err)
	}
	return nil
}

func (s *Store) Pool() *pgxpool.Pool { return s.pool }

func Migrate(ctx context.Context, url string) error {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return err
	}
	db := stdlib.OpenDB(*cfg.ConnConfig)
	defer db.Close()
	return migrateDB(ctx, db)
}

func migrateDB(ctx context.Context, db *sql.DB) error {
	migrateMu.Lock()
	defer migrateMu.Unlock()
	if _, err := db.ExecContext(ctx, `
		create table if not exists schema_migrations (
			filename text primary key,
			applied_at timestamptz not null default now()
		)`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(embedMigrations, "migrations")
	if err != nil {
		return err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	for _, name := range files {
		var exists bool
		if err := db.QueryRowContext(ctx, `select exists(select 1 from schema_migrations where filename=$1)`, name).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		raw, err := embedMigrations.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		up := extractUp(string(raw))
		if _, err := db.ExecContext(ctx, up); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := db.ExecContext(ctx, `insert into schema_migrations (filename) values ($1)`, name); err != nil {
			return err
		}
	}
	return nil
}

func extractUp(sqlText string) string {
	up := sqlText
	if i := strings.Index(sqlText, "-- +goose Up"); i >= 0 {
		up = sqlText[i+len("-- +goose Up"):]
	}
	if j := strings.Index(up, "-- +goose Down"); j >= 0 {
		up = up[:j]
	}
	return strings.TrimSpace(up)
}

func (s *Store) Migrate(ctx context.Context) error {
	db := stdlib.OpenDBFromPool(s.pool)
	defer db.Close()
	return migrateDB(ctx, db)
}

func isUnavailable(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// Class 08 = connection exception; 57P01 admin shutdown etc.
		if len(pgErr.Code) >= 2 && pgErr.Code[:2] == "08" {
			return true
		}
	}
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

func wrapStore(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, lease.ErrNotFound) || errors.Is(err, lease.ErrNotHolder) || errors.Is(err, lease.ErrGone) || errors.Is(err, lease.ErrInvalidInput) {
		return err
	}
	var gone *lease.GoneError
	if errors.As(err, &gone) {
		return err
	}
	if isUnavailable(err) {
		return fmt.Errorf("%w: %v", lease.ErrUnavailable, err)
	}
	return fmt.Errorf("%w: %v", lease.ErrUnavailable, err)
}

type auditRow struct {
	typ, customerID, principalType, principalID, domainKey, executionID, ruleName, reason, requestID string
	fence                                                                                            *int64
}

func insertAudit(ctx context.Context, tx pgx.Tx, merchantID string, a auditRow) error {
	_, err := tx.Exec(ctx, `
		insert into audit_events (
			merchant_id, type, customer_id, principal_type, principal_id,
			domain_key, execution_id, fence, rule_name, reason, request_id
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		merchantID, a.typ, nullIfEmpty(a.customerID), nullIfEmpty(a.principalType),
		nullIfEmpty(a.principalID), nullIfEmpty(a.domainKey), nullIfEmpty(a.executionID),
		a.fence, nullIfEmpty(a.ruleName), nullIfEmpty(a.reason), nullIfEmpty(a.requestID),
	)
	return err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
