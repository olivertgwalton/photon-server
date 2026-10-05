-- +goose Up
CREATE TABLE leader (
  name text PRIMARY KEY,
  node_id uuid NOT NULL,
  expires_at timestamptz NOT NULL
);

CREATE TABLE task_state (
  key text PRIMARY KEY CONSTRAINT task_key CHECK (key IN ('scan_libraries')),
  started_at timestamptz NOT NULL,
  finished_at timestamptz,
  result text CONSTRAINT task_result CHECK (result IN ('succeeded', 'failed', 'cancelled')),
  error text
);

-- +goose Down
DROP TABLE task_state, leader;
