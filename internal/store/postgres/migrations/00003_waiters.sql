-- +goose Up
create table waiters (
    waiter_id      text primary key,
    merchant_id    text not null,
    domain_key     text not null,
    customer_id    text not null,
    principal_type text not null,
    principal_id   text not null,
    session_id     text not null,
    resource       text not null,
    action         text not null,
    rule_name      text not null,
    max_active     int not null default 1,
    lease_ttl_ms   bigint not null default 60000,
    max_lifetime_ms bigint not null default 900000,
    created_at     timestamptz not null default now(),
    expires_at     timestamptz not null
);
create unique index waiters_domain_principal
    on waiters (merchant_id, domain_key, principal_type, principal_id);
create index waiters_domain_fifo
    on waiters (merchant_id, domain_key, created_at);

-- +goose Down
drop table if exists waiters;
