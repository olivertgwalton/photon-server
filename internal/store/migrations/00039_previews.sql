-- +goose Up
ALTER TABLE libraries ADD COLUMN previews text NOT NULL DEFAULT 'all'
  CONSTRAINT preview_level CHECK (previews IN ('off', 'chapters', 'all'));

-- A part's previews, made as its library asks; the files are in the cache's previews folder. A
-- part with a row has had its chapter images made (chapter_images are the idx of those that have
-- one), and its trickplay sheets too where it has a trickplay row.
CREATE TABLE previews (
  part_id uuid PRIMARY KEY REFERENCES parts(id) ON DELETE CASCADE,
  chapter_images int[] NOT NULL
);

CREATE TABLE trickplay (
  part_id uuid PRIMARY KEY REFERENCES previews(part_id) ON DELETE CASCADE,
  width int NOT NULL,
  height int NOT NULL,
  interval_ms int NOT NULL,
  columns int NOT NULL,
  rows int NOT NULL,
  thumbnails int NOT NULL
);

ALTER TABLE jobs DROP CONSTRAINT job_kind,
  ADD CONSTRAINT job_kind CHECK (kind IN ('keyframes', 'identify', 'scan_library', 'markers', 'previews'));
ALTER TABLE task_state DROP CONSTRAINT task_key,
  ADD CONSTRAINT task_key CHECK (key IN ('scan_libraries', 'sweep_jobs', 'backup_database', 'refresh_metadata', 'sweep_artwork', 'detect_markers', 'backfill_previews'));

-- +goose Down
DELETE FROM task_state WHERE key = 'backfill_previews';
ALTER TABLE task_state DROP CONSTRAINT task_key,
  ADD CONSTRAINT task_key CHECK (key IN ('scan_libraries', 'sweep_jobs', 'backup_database', 'refresh_metadata', 'sweep_artwork', 'detect_markers'));
DELETE FROM jobs WHERE kind = 'previews';
ALTER TABLE jobs DROP CONSTRAINT job_kind,
  ADD CONSTRAINT job_kind CHECK (kind IN ('keyframes', 'identify', 'scan_library', 'markers'));
DROP TABLE trickplay, previews;
ALTER TABLE libraries DROP COLUMN previews;
