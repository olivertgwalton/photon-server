-- +goose NO TRANSACTION
-- +goose Up
-- A request that waits is pushed, by a job of its own, to the browsers of those who decide it. The
-- jobs check is checked again apart from its change, reading the table without stopping its writes.
ALTER TABLE jobs DROP CONSTRAINT job_kind, ADD CONSTRAINT job_kind CHECK (kind IN ('keyframes',
  'keyframe_walk', 'identify', 'scan_library', 'markers', 'previews', 'convert', 'deliver_webhook',
  'theme', 'probe', 'import_history', 'segments', 'push', 'send_request', 'push_request')) NOT VALID;
ALTER TABLE jobs VALIDATE CONSTRAINT job_kind;

-- +goose Down
DELETE FROM jobs WHERE kind = 'push_request';
ALTER TABLE jobs DROP CONSTRAINT job_kind, ADD CONSTRAINT job_kind CHECK (kind IN ('keyframes',
  'keyframe_walk', 'identify', 'scan_library', 'markers', 'previews', 'convert', 'deliver_webhook',
  'theme', 'probe', 'import_history', 'segments', 'push', 'send_request')) NOT VALID;
ALTER TABLE jobs VALIDATE CONSTRAINT job_kind;
