-- +goose NO TRANSACTION
-- +goose Up
-- A library of files may name a provider its approved requests are sent to, by a job for each. A
-- request sent is sent, or failed, saying why, for an admin to approve again or decline. The jobs
-- check is checked again apart from its change, reading the table without stopping its writes.
ALTER TABLE libraries ADD COLUMN request_source text,
  ADD CONSTRAINT library_requests CHECK (request_source IS NULL OR media = 'folder');
ALTER TABLE requests DROP CONSTRAINT request_state,
  ADD CONSTRAINT request_state CHECK (state IN ('pending', 'approved', 'declined', 'sent', 'failed'));
ALTER TABLE jobs DROP CONSTRAINT job_kind, ADD CONSTRAINT job_kind CHECK (kind IN ('keyframes',
  'keyframe_walk', 'identify', 'scan_library', 'markers', 'previews', 'convert', 'deliver_webhook',
  'theme', 'probe', 'import_history', 'segments', 'push', 'send_request')) NOT VALID;
ALTER TABLE jobs VALIDATE CONSTRAINT job_kind;

-- +goose Down
DELETE FROM jobs WHERE kind = 'send_request';
ALTER TABLE jobs DROP CONSTRAINT job_kind, ADD CONSTRAINT job_kind CHECK (kind IN ('keyframes',
  'keyframe_walk', 'identify', 'scan_library', 'markers', 'previews', 'convert', 'deliver_webhook',
  'theme', 'probe', 'import_history', 'segments', 'push'));
UPDATE requests SET state = 'approved' WHERE state IN ('sent', 'failed');
ALTER TABLE requests DROP CONSTRAINT request_state,
  ADD CONSTRAINT request_state CHECK (state IN ('pending', 'approved', 'declined'));
ALTER TABLE libraries DROP CONSTRAINT library_requests, DROP COLUMN request_source;
