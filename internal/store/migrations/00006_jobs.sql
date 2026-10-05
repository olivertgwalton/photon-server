-- +goose Up
CREATE TABLE jobs (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  kind text NOT NULL CONSTRAINT job_kind CHECK (kind IN ('keyframes')),
  subject uuid NOT NULL,
  state text NOT NULL DEFAULT 'queued' CONSTRAINT job_state CHECK (state IN ('queued', 'running', 'dead')),
  priority smallint NOT NULL DEFAULT 0,
  attempts smallint NOT NULL DEFAULT 0,
  run_after timestamptz NOT NULL DEFAULT now(),
  lease_until timestamptz,
  node_id uuid,
  last_error text,
  UNIQUE (kind, subject)
);
CREATE INDEX jobs_claim ON jobs (priority DESC, id) WHERE state = 'queued';

CREATE TABLE keyframes (
  part_id uuid PRIMARY KEY REFERENCES parts(id) ON DELETE CASCADE,
  pts_ms bigint[] NOT NULL
);

ALTER TABLE task_state DROP CONSTRAINT task_key;
ALTER TABLE task_state ADD CONSTRAINT task_key CHECK (key IN ('scan_libraries', 'sweep_jobs'));

-- +goose Down
ALTER TABLE task_state DROP CONSTRAINT task_key;
ALTER TABLE task_state ADD CONSTRAINT task_key CHECK (key IN ('scan_libraries'));
DROP TABLE keyframes, jobs;
