-- +goose Up
-- A conversion is one part made smaller for downloading, shared by every profile asking for the
-- part at the same quality, as Plex's sync cache is. Its file is on the node that made it.
CREATE TABLE conversions (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  part_id uuid NOT NULL REFERENCES parts(id) ON DELETE CASCADE,
  max_bitrate_kbps int NOT NULL CHECK (max_bitrate_kbps > 0),
  max_width int NOT NULL CHECK (max_width >= 0),
  state text NOT NULL DEFAULT 'queued'
    CONSTRAINT download_state CHECK (state IN ('queued', 'converting', 'ready', 'failed')),
  progress real NOT NULL DEFAULT 0 CHECK (progress BETWEEN 0 AND 1),
  size_bytes bigint,
  error text,
  node_id uuid,
  finished_at timestamptz,
  UNIQUE (part_id, max_bitrate_kbps, max_width)
);

-- A download is a profile's ask for a part: its own file where that fits the quality asked for
-- (no conversion), else a conversion. Asking twice answers the same download.
CREATE TABLE downloads (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  part_id uuid NOT NULL REFERENCES parts(id) ON DELETE CASCADE,
  conversion_id uuid REFERENCES conversions(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE NULLS NOT DISTINCT (profile_id, part_id, conversion_id)
);
CREATE INDEX downloads_conversion ON downloads (conversion_id);

ALTER TABLE jobs DROP CONSTRAINT job_kind,
  ADD CONSTRAINT job_kind CHECK (kind IN ('keyframes', 'identify', 'scan_library', 'markers', 'previews', 'convert'));
ALTER TABLE task_state DROP CONSTRAINT task_key,
  ADD CONSTRAINT task_key CHECK (key IN ('scan_libraries', 'sweep_jobs', 'backup_database', 'refresh_metadata', 'sweep_artwork', 'detect_markers', 'backfill_previews', 'sweep_downloads'));

-- +goose Down
DELETE FROM task_state WHERE key = 'sweep_downloads';
ALTER TABLE task_state DROP CONSTRAINT task_key,
  ADD CONSTRAINT task_key CHECK (key IN ('scan_libraries', 'sweep_jobs', 'backup_database', 'refresh_metadata', 'sweep_artwork', 'detect_markers', 'backfill_previews'));
DELETE FROM jobs WHERE kind = 'convert';
ALTER TABLE jobs DROP CONSTRAINT job_kind,
  ADD CONSTRAINT job_kind CHECK (kind IN ('keyframes', 'identify', 'scan_library', 'markers', 'previews'));
DROP TABLE downloads, conversions;
