-- +goose NO TRANSACTION
-- +goose Up
-- The sweeper's leases run out only on jobs being run, and an admin is shown the latest dead jobs;
-- neither is a queued job, the only kind jobs_claim holds.
CREATE INDEX CONCURRENTLY IF NOT EXISTS jobs_lease ON jobs (lease_until) WHERE state IN ('running', 'rerun');
CREATE INDEX CONCURRENTLY IF NOT EXISTS jobs_dead ON jobs (id) WHERE state = 'dead';

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS jobs_dead;
DROP INDEX CONCURRENTLY IF EXISTS jobs_lease;
