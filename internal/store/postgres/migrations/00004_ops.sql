-- +goose Up
create table if not exists runtime_controls (
    merchant_id         text primary key,
    mode                text not null default 'enforce',
    enforcement         boolean not null default true,
    queue_enabled       boolean not null default true,
    max_waiters         int not null default 0,
    lease_ttl_seconds   int not null default 0,
    fail_closed         boolean not null default true,
    disabled_actions    text not null default '',
    updated_at          timestamptz not null default now(),
    updated_by          text
);

create table if not exists dry_run_holds (
    hold_id      text primary key,
    merchant_id  text not null,
    domain_key   text not null,
    customer_id  text not null,
    principal_id text not null,
    resource     text not null,
    action       text not null,
    rule_name    text not null,
    kind         text not null,
    created_at   timestamptz not null default now(),
    expires_at   timestamptz not null
);
create index if not exists dry_run_holds_domain on dry_run_holds (merchant_id, domain_key, kind);

create table if not exists dry_run_events (
    seq          bigserial primary key,
    merchant_id  text not null,
    at           timestamptz not null default now(),
    customer_id  text,
    principal_id text,
    resource     text,
    action       text,
    would        text not null,
    reason       text,
    rule_name    text,
    request_id   text
);
create index if not exists dry_run_events_merchant_at on dry_run_events (merchant_id, at desc);

-- +goose Down
drop table if exists dry_run_events;
drop table if exists dry_run_holds;
drop table if exists runtime_controls;
