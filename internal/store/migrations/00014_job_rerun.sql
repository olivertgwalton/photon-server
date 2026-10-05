-- +goose Up
ALTER TABLE jobs DROP CONSTRAINT job_state,
  ADD CONSTRAINT job_state CHECK (state IN ('queued', 'running', 'rerun', 'dead'));

-- +goose Down
UPDATE jobs SET state = 'queued' WHERE state = 'rerun';
ALTER TABLE jobs DROP CONSTRAINT job_state,
  ADD CONSTRAINT job_state CHECK (state IN ('queued', 'running', 'dead'));
