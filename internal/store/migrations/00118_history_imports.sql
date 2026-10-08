-- +goose NO TRANSACTION
-- +goose Up
-- A profile's watch history imported from a Plex, Jellyfin or Emby server, by a job of its own. The
-- source's user and token are kept only while the import runs: a password never is.
CREATE TABLE IF NOT EXISTS history_imports (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  source text NOT NULL CONSTRAINT import_source CHECK (source IN ('plex', 'jellyfin', 'emby')),
  url text NOT NULL,
  profile_id uuid NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  source_user text NOT NULL DEFAULT '',
  token text,
  status text NOT NULL DEFAULT 'queued'
    CONSTRAINT import_status CHECK (status IN ('queued', 'running', 'done', 'failed')),
  error text NOT NULL DEFAULT '',
  matched integer NOT NULL DEFAULT 0,
  imported integer NOT NULL DEFAULT 0,
  skipped integer NOT NULL DEFAULT 0,
  unmatched integer NOT NULL DEFAULT 0,
  misses jsonb NOT NULL DEFAULT '[]',
  created_at timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz
);
CREATE INDEX IF NOT EXISTS history_imports_profile ON history_imports (profile_id);

ALTER TABLE jobs DROP CONSTRAINT IF EXISTS job_kind, ADD CONSTRAINT job_kind CHECK (kind IN (
  'keyframes', 'keyframe_walk', 'identify', 'scan_library', 'markers', 'previews', 'convert', 'deliver_webhook', 'theme',
  'probe', 'import_history')) NOT VALID;
ALTER TABLE jobs VALIDATE CONSTRAINT job_kind;

-- +goose Down
DELETE FROM jobs WHERE kind = 'import_history';
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS job_kind, ADD CONSTRAINT job_kind CHECK (kind IN (
  'keyframes', 'keyframe_walk', 'identify', 'scan_library', 'markers', 'previews', 'convert', 'deliver_webhook', 'theme',
  'probe')) NOT VALID;
ALTER TABLE jobs VALIDATE CONSTRAINT job_kind;
DROP TABLE IF EXISTS history_imports;
