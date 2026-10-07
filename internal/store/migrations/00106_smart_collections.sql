-- +goose NO TRANSACTION
-- +goose Up
-- A smart collection is a library's titles as a rule narrows and orders them, as Plex's: its
-- members are kept, made again as the library changes. Its checks are validated apart, reading the
-- tables without stopping their writes.
ALTER TABLE collections ADD COLUMN IF NOT EXISTS rule jsonb;
ALTER TABLE collections DROP CONSTRAINT IF EXISTS collection_origin,
  ADD CONSTRAINT collection_origin CHECK (origin IN ('tmdb', 'user', 'smart')) NOT VALID;
ALTER TABLE collections VALIDATE CONSTRAINT collection_origin;
ALTER TABLE collections DROP CONSTRAINT IF EXISTS collection_rule,
  ADD CONSTRAINT collection_rule CHECK ((origin = 'smart') = (rule IS NOT NULL)) NOT VALID;
ALTER TABLE collections VALIDATE CONSTRAINT collection_rule;
ALTER TABLE task_state DROP CONSTRAINT IF EXISTS task_key,
  ADD CONSTRAINT task_key CHECK (key IN ('scan_libraries', 'sweep_jobs', 'backup_database', 'refresh_metadata',
    'sweep_artwork', 'detect_markers', 'backfill_previews', 'sweep_downloads', 'prune_activity', 'refresh_collections')) NOT VALID;
ALTER TABLE task_state VALIDATE CONSTRAINT task_key;

-- +goose Down
DELETE FROM task_state WHERE key = 'refresh_collections';
ALTER TABLE task_state DROP CONSTRAINT IF EXISTS task_key,
  ADD CONSTRAINT task_key CHECK (key IN ('scan_libraries', 'sweep_jobs', 'backup_database', 'refresh_metadata',
    'sweep_artwork', 'detect_markers', 'backfill_previews', 'sweep_downloads', 'prune_activity'));
DELETE FROM items WHERE id IN (SELECT item_id FROM collections WHERE origin = 'smart');
ALTER TABLE collections DROP CONSTRAINT IF EXISTS collection_rule, DROP COLUMN IF EXISTS rule;
ALTER TABLE collections DROP CONSTRAINT IF EXISTS collection_origin,
  ADD CONSTRAINT collection_origin CHECK (origin IN ('tmdb', 'user'));
