-- +goose Up
create table merchants (
    merchant_id     text primary key,
    name            text not null,
    idp_hmac_secret text,
    created_at      timestamptz not null default now()
);

create table signing_keys (
    kid          text primary key,
    merchant_id  text not null references merchants (merchant_id),
    public_key   bytea not null,
    private_key  bytea not null,
    created_at   timestamptz not null default now(),
    retired_at   timestamptz
);
create index signing_keys_merchant on signing_keys (merchant_id) where retired_at is null;

create table sessions (
    session_id     text primary key,
    merchant_id    text not null,
    customer_id    text not null,
    anchors        jsonb not null default '{}',
    principal_type text not null,
    principal_id   text not null,
    expires_at     timestamptz not null,
    created_at     timestamptz not null default now()
);
create index sessions_merchant_customer on sessions (merchant_id, customer_id);

create table domains (
    merchant_id text not null,
    domain_key  text not null,
    fence       bigint not null default 0,
    updated_at  timestamptz not null default now(),
    primary key (merchant_id, domain_key)
);

create table executions (
    execution_id    text primary key,
    merchant_id     text not null,
    domain_key      text not null,
    customer_id     text not null,
    principal_type  text not null,
    principal_id    text not null,
    session_id      text not null,
    resource        text not null,
    action          text not null,
    rule_name       text not null,
    fence           bigint not null,
    state           text not null,
    granted_at      timestamptz not null,
    expires_at      timestamptz not null,
    max_lifetime_at timestamptz not null,
    renew_count     int not null default 0,
    ended_at        timestamptz,
    end_reason      text,
    successor_id    text
);
create index executions_active on executions (merchant_id, domain_key) where state = 'ACTIVE';
create index executions_customer on executions (merchant_id, customer_id, granted_at desc);

create table audit_events (
    seq            bigserial primary key,
    merchant_id    text not null,
    at             timestamptz not null default now(),
    type           text not null,
    customer_id    text,
    principal_type text,
    principal_id   text,
    domain_key     text,
    execution_id   text,
    fence          bigint,
    rule_name      text,
    reason         text,
    request_id     text,
    attrs          jsonb not null default '{}'
);
create index audit_events_merchant_at on audit_events (merchant_id, at desc);
create index audit_events_execution on audit_events (execution_id);

-- +goose Down
drop table if exists audit_events;
drop table if exists executions;
drop table if exists domains;
drop table if exists sessions;
drop table if exists signing_keys;
drop table if exists merchants;
