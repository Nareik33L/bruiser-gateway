-- +goose Up
create table policies (
    merchant_id text not null,
    version     int not null,
    yaml        text not null,
    active      boolean not null default false,
    created_at  timestamptz not null default now(),
    primary key (merchant_id, version)
);
create unique index policies_one_active on policies (merchant_id) where active;

-- +goose Down
drop table if exists policies;
