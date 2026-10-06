-- +goose NO TRANSACTION
-- +goose Up
-- When each job is meant to run: as soon as there is room, or in the maintenance window, as the
-- window's backfill queues its work. Each statement may be run again, should one fail part way.
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS due text NOT NULL DEFAULT 'now';
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS job_due, ADD CONSTRAINT job_due CHECK (due IN ('now', 'window')) NOT VALID;
ALTER TABLE jobs VALIDATE CONSTRAINT job_due;

-- +goose Down
ALTER TABLE jobs DROP COLUMN IF EXISTS due;
