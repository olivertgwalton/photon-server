-- +goose NO TRANSACTION
-- +goose Up
-- The languages a library fetches subtitles in for copies with none in them, as Jellyfin's
-- "Download languages", and which it takes: one made for a copy's very file, as Jellyfin's "Only
-- download subtitles that are a perfect match", or the best of any.
ALTER TABLE libraries
  ADD COLUMN IF NOT EXISTS subtitle_languages text[] NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS subtitle_match text NOT NULL DEFAULT 'release';
ALTER TABLE libraries DROP CONSTRAINT IF EXISTS subtitle_match,
  ADD CONSTRAINT subtitle_match CHECK (subtitle_match IN ('release', 'any')) NOT VALID;
ALTER TABLE libraries VALIDATE CONSTRAINT subtitle_match;
-- When a copy was last searched for a subtitle in a language, so one with none to be had is not
-- searched for again each day.
CREATE TABLE IF NOT EXISTS subtitle_searches (
  version_id uuid NOT NULL REFERENCES versions(id) ON DELETE CASCADE,
  language text NOT NULL,
  searched_at timestamptz NOT NULL,
  PRIMARY KEY (version_id, language)
);
ALTER TABLE task_state DROP CONSTRAINT IF EXISTS task_key,
  ADD CONSTRAINT task_key CHECK (key IN ('scan_libraries', 'sweep_jobs', 'backup_database', 'refresh_metadata',
    'sweep_artwork', 'detect_markers', 'backfill_previews', 'sweep_downloads', 'prune_activity', 'refresh_collections',
    'sync_lists', 'fetch_subtitles')) NOT VALID;
ALTER TABLE task_state VALIDATE CONSTRAINT task_key;

-- +goose Down
DELETE FROM task_state WHERE key = 'fetch_subtitles';
ALTER TABLE task_state DROP CONSTRAINT IF EXISTS task_key,
  ADD CONSTRAINT task_key CHECK (key IN ('scan_libraries', 'sweep_jobs', 'backup_database', 'refresh_metadata',
    'sweep_artwork', 'detect_markers', 'backfill_previews', 'sweep_downloads', 'prune_activity', 'refresh_collections',
    'sync_lists'));
DROP TABLE IF EXISTS subtitle_searches;
ALTER TABLE libraries DROP COLUMN IF EXISTS subtitle_languages, DROP COLUMN IF EXISTS subtitle_match;
