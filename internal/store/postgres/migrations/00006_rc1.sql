-- +goose Up
alter table sessions add column if not exists revoked_at timestamptz;
alter table sessions add column if not exists version int not null default 1;

create table if not exists execution_budget (
    execution_id text primary key,
    merchant_id  text not null,
    max_ops      int not null default 0,
    ops_used     int not null default 0
);
create index if not exists execution_budget_merchant on execution_budget (merchant_id);

create table if not exists replay_keys (
    merchant_id text not null,
    replay_key  text not null,
    kind        text not null,
    expires_at  timestamptz not null,
    primary key (merchant_id, replay_key)
);
create index if not exists replay_keys_exp on replay_keys (expires_at);

-- +goose Down
drop table if exists replay_keys;
drop table if exists execution_budget;
alter table sessions drop column if exists version;
alter table sessions drop column if exists revoked_at;
