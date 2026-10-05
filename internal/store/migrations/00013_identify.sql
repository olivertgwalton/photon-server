-- +goose Up
ALTER TABLE jobs DROP CONSTRAINT job_kind,
  ADD CONSTRAINT job_kind CHECK (kind IN ('keyframes', 'identify'));
ALTER TABLE external_ids DROP CONSTRAINT id_source,
  ADD CONSTRAINT id_source CHECK (source IN ('match', 'nfo', 'path'));
INSERT INTO jobs (kind, subject) SELECT 'identify', id FROM items WHERE kind IN ('movie', 'show');

-- +goose Down
DELETE FROM jobs WHERE kind = 'identify';
DELETE FROM external_ids WHERE source = 'match';
ALTER TABLE jobs DROP CONSTRAINT job_kind,
  ADD CONSTRAINT job_kind CHECK (kind IN ('keyframes'));
ALTER TABLE external_ids DROP CONSTRAINT id_source,
  ADD CONSTRAINT id_source CHECK (source IN ('nfo', 'path'));
