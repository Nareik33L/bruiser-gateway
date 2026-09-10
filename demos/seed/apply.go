package seed

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const HarchesterSchema = `
-- DEMO ONLY - plaintext passwords by design - never copy this schema
create table if not exists supporters (
    membership_number   text primary key,
    customer_id         text unique not null,
    first_name          text not null,
    last_name           text not null,
    email               text not null,
    password            text not null, -- DEMO ONLY - plaintext by design - never copy this schema
    membership_tier     text not null,
    loyalty_points      int not null,
    season_ticket       boolean not null,
    eligible_for_arsenal boolean not null
);
comment on table supporters is 'DEMO ONLY - plaintext passwords by design - never copy this schema';
comment on column supporters.password is 'DEMO ONLY - plaintext by design - never copy this schema';

create table if not exists club_sessions (
    id                 text primary key,
    membership_number  text not null references supporters (membership_number),
    created_at         timestamptz not null default now(),
    expires_at         timestamptz not null,
    last_seen          timestamptz not null default now()
);
create index if not exists club_sessions_member on club_sessions (membership_number);
`

func ApplyHarchester(ctx context.Context, pool *pgxpool.Pool, n int) error {
	if _, err := pool.Exec(ctx, HarchesterSchema); err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `select count(*) from supporters`).Scan(&count); err != nil {
		return err
	}
	if count >= n {
		return overlayNamedSQL(ctx, pool)
	}
	all := GenerateSupporters(n)
	batch := &pgx.Batch{}
	const q = `
		insert into supporters (
			membership_number, customer_id, first_name, last_name, email, password,
			membership_tier, loyalty_points, season_ticket, eligible_for_arsenal
		) values ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		on conflict (membership_number) do nothing`
	for i, s := range all {
		batch.Queue(q, s.MembershipNumber, s.CustomerID, s.FirstName, s.LastName, s.Email, s.Password,
			s.MembershipTier, s.LoyaltyPoints, s.SeasonTicket, s.EligibleForArsenal)
		if (i+1)%500 == 0 {
			if err := execBatch(ctx, pool, batch); err != nil {
				return err
			}
			batch = &pgx.Batch{}
		}
	}
	if batch.Len() > 0 {
		if err := execBatch(ctx, pool, batch); err != nil {
			return err
		}
	}
	return overlayNamedSQL(ctx, pool)
}

func overlayNamedSQL(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `
		update supporters set
		  first_name='Alice', last_name='Okafor', email='alice.okafor@example.com',
		  membership_tier='Gold', season_ticket=true, eligible_for_arsenal=true, loyalty_points=4820
		where membership_number=$1`, AliceMembership)
	if err != nil {
		return err
	}
	_, err = pool.Exec(ctx, `
		update supporters set
		  first_name='Sam', last_name='Quinn', email='sam.quinn@example.com',
		  membership_tier='Junior', season_ticket=false, eligible_for_arsenal=false, loyalty_points=120
		where membership_number=$1`, IneligibleMember)
	return err
}

func execBatch(ctx context.Context, pool *pgxpool.Pool, batch *pgx.Batch) error {
	br := pool.SendBatch(ctx, batch)
	defer br.Close()
	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}
	return nil
}
