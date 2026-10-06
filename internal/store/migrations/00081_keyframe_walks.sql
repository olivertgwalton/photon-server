-- +goose NO TRANSACTION
-- +goose Up
-- A file with no keyframe index is walked through by a job of its own, in the maintenance window.
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS job_kind, ADD CONSTRAINT job_kind CHECK (kind IN (
  'keyframes', 'keyframe_walk', 'identify', 'scan_library', 'markers', 'previews', 'convert', 'deliver_webhook', 'theme')) NOT VALID;
ALTER TABLE jobs VALIDATE CONSTRAINT job_kind;

-- +goose Down
DELETE FROM jobs WHERE kind = 'keyframe_walk';
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS job_kind, ADD CONSTRAINT job_kind CHECK (kind IN (
  'keyframes', 'identify', 'scan_library', 'markers', 'previews', 'convert', 'deliver_webhook', 'theme')) NOT VALID;
ALTER TABLE jobs VALIDATE CONSTRAINT job_kind;
