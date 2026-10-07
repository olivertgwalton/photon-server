-- +goose NO TRANSACTION
-- +goose Up
-- A file's streams are read again when an admin analyses its title, by a job of its own.
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS job_kind, ADD CONSTRAINT job_kind CHECK (kind IN (
  'keyframes', 'keyframe_walk', 'identify', 'scan_library', 'markers', 'previews', 'convert', 'deliver_webhook', 'theme',
  'probe')) NOT VALID;
ALTER TABLE jobs VALIDATE CONSTRAINT job_kind;

-- +goose Down
DELETE FROM jobs WHERE kind = 'probe';
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS job_kind, ADD CONSTRAINT job_kind CHECK (kind IN (
  'keyframes', 'keyframe_walk', 'identify', 'scan_library', 'markers', 'previews', 'convert', 'deliver_webhook', 'theme'))
  NOT VALID;
ALTER TABLE jobs VALIDATE CONSTRAINT job_kind;
