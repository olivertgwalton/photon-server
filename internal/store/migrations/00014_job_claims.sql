-- +goose NO TRANSACTION
-- +goose Up
-- A worker claims jobs of its own kinds, of those it may run now. Ordered by priority alone, the
-- queue is read past every other kind's jobs to find its own: a large import's previews and
-- keyframes, a million rows, on every poll of every worker.
CREATE INDEX CONCURRENTLY IF NOT EXISTS jobs_queued ON jobs (kind, due, priority DESC, id) WHERE state = 'queued';
DROP INDEX CONCURRENTLY IF EXISTS jobs_claim;

-- +goose Down
CREATE INDEX CONCURRENTLY IF NOT EXISTS jobs_claim ON jobs (priority DESC, id) WHERE state = 'queued';
DROP INDEX CONCURRENTLY IF EXISTS jobs_queued;
