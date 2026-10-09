-- +goose NO TRANSACTION
-- +goose Up
-- A plugin times a film's or an episode's intro and credits, in a job of its own, once for each
-- part. Each check is checked again apart from its change, reading the table without stopping its
-- writes.
ALTER TABLE jobs DROP CONSTRAINT job_kind, ADD CONSTRAINT job_kind CHECK (kind IN ('keyframes',
  'keyframe_walk', 'identify', 'scan_library', 'markers', 'previews', 'convert', 'deliver_webhook',
  'theme', 'probe', 'import_history', 'segments')) NOT VALID;
ALTER TABLE jobs VALIDATE CONSTRAINT job_kind;
ALTER TABLE markers DROP CONSTRAINT marker_source, ADD CONSTRAINT marker_source
  CHECK (source IN ('user', 'chapter', 'provider', 'fingerprint', 'blackframes')) NOT VALID;
ALTER TABLE markers VALIDATE CONSTRAINT marker_source;
ALTER TABLE parts ADD COLUMN segments_asked_at timestamptz;

-- +goose Down
ALTER TABLE parts DROP COLUMN segments_asked_at;
DELETE FROM markers WHERE source = 'provider';
ALTER TABLE markers DROP CONSTRAINT marker_source, ADD CONSTRAINT marker_source
  CHECK (source IN ('user', 'chapter', 'fingerprint', 'blackframes'));
DELETE FROM jobs WHERE kind = 'segments';
ALTER TABLE jobs DROP CONSTRAINT job_kind, ADD CONSTRAINT job_kind CHECK (kind IN ('keyframes',
  'keyframe_walk', 'identify', 'scan_library', 'markers', 'previews', 'convert', 'deliver_webhook',
  'theme', 'probe', 'import_history'));
