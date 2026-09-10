-- +goose Up
alter table runtime_controls add column if not exists enforce_percent int not null default 100;
alter table runtime_controls add column if not exists ramp_salt text not null default '';
alter table runtime_controls add column if not exists ramp_scope text not null default '{}';

alter table dry_run_events add column if not exists enforced boolean not null default false;
alter table dry_run_events add column if not exists error text;

-- +goose Down
alter table dry_run_events drop column if exists error;
alter table dry_run_events drop column if exists enforced;
alter table runtime_controls drop column if exists ramp_scope;
alter table runtime_controls drop column if exists ramp_salt;
alter table runtime_controls drop column if exists enforce_percent;
