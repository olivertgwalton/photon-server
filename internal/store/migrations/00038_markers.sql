-- +goose Up
-- The stretches of a part a player may offer to skip, from the part's start. A part's chapters
-- are read for them as the part is answered, so only what an admin says and what a season's
-- fingerprints found are kept.
CREATE TABLE markers (
  part_id uuid NOT NULL REFERENCES parts(id) ON DELETE CASCADE,
  kind text NOT NULL CONSTRAINT marker_kind CHECK (kind IN ('intro', 'credits', 'recap', 'preview')),
  source text NOT NULL CONSTRAINT marker_source CHECK (source IN ('user', 'chapter', 'fingerprint')),
  start_ms bigint NOT NULL CHECK (start_ms >= 0),
  end_ms bigint NOT NULL CHECK (end_ms > start_ms),
  PRIMARY KEY (part_id, kind, source)
);
-- When the part's sound was last compared with its season's.
ALTER TABLE parts ADD COLUMN fingerprinted_at timestamptz;
ALTER TABLE jobs DROP CONSTRAINT job_kind,
  ADD CONSTRAINT job_kind CHECK (kind IN ('keyframes', 'identify', 'scan_library', 'markers'));
ALTER TABLE task_state DROP CONSTRAINT task_key,
  ADD CONSTRAINT task_key CHECK (key IN ('scan_libraries', 'sweep_jobs', 'backup_database', 'refresh_metadata', 'sweep_artwork', 'detect_markers'));

-- +goose Down
DELETE FROM task_state WHERE key = 'detect_markers';
ALTER TABLE task_state DROP CONSTRAINT task_key,
  ADD CONSTRAINT task_key CHECK (key IN ('scan_libraries', 'sweep_jobs', 'backup_database', 'refresh_metadata', 'sweep_artwork'));
DELETE FROM jobs WHERE kind = 'markers';
ALTER TABLE jobs DROP CONSTRAINT job_kind,
  ADD CONSTRAINT job_kind CHECK (kind IN ('keyframes', 'identify', 'scan_library'));
ALTER TABLE parts DROP COLUMN fingerprinted_at;
DROP TABLE markers;
