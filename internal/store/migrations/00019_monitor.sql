-- +goose Up
ALTER TABLE libraries ADD COLUMN monitor text NOT NULL DEFAULT 'realtime'
  CONSTRAINT monitor CHECK (monitor IN ('realtime', 'off'));
ALTER TABLE jobs DROP CONSTRAINT job_kind,
  ADD CONSTRAINT job_kind CHECK (kind IN ('keyframes', 'identify', 'scan_library'));

-- +goose Down
DELETE FROM jobs WHERE kind = 'scan_library';
ALTER TABLE jobs DROP CONSTRAINT job_kind,
  ADD CONSTRAINT job_kind CHECK (kind IN ('keyframes', 'identify'));
ALTER TABLE libraries DROP COLUMN monitor;
