-- +goose NO TRANSACTION
-- +goose Up
-- A list collection holds a list kept on a provider, its titles found by their ids, as Kometa's
-- list builders: which provider and list, and how many of its titles the library lacks. Titles
-- are found by an id, so the ids are indexed by value. The checks are validated apart and the
-- index built concurrently, so the tables are read without stopping their writes.
ALTER TABLE collections
  ADD COLUMN IF NOT EXISTS list_source text,
  ADD COLUMN IF NOT EXISTS list_id text,
  ADD COLUMN IF NOT EXISTS list_missing integer NOT NULL DEFAULT 0;
ALTER TABLE collections DROP CONSTRAINT IF EXISTS collection_origin,
  ADD CONSTRAINT collection_origin CHECK (origin IN ('tmdb', 'user', 'smart', 'list')) NOT VALID;
ALTER TABLE collections VALIDATE CONSTRAINT collection_origin;
ALTER TABLE collections DROP CONSTRAINT IF EXISTS collection_list,
  ADD CONSTRAINT collection_list CHECK ((origin = 'list') = (list_source IS NOT NULL AND list_id IS NOT NULL)) NOT VALID;
ALTER TABLE collections VALIDATE CONSTRAINT collection_list;
CREATE INDEX CONCURRENTLY IF NOT EXISTS external_ids_value ON external_ids (provider, value);
ALTER TABLE task_state DROP CONSTRAINT IF EXISTS task_key,
  ADD CONSTRAINT task_key CHECK (key IN ('scan_libraries', 'sweep_jobs', 'backup_database', 'refresh_metadata',
    'sweep_artwork', 'detect_markers', 'backfill_previews', 'sweep_downloads', 'prune_activity', 'refresh_collections',
    'sync_lists')) NOT VALID;
ALTER TABLE task_state VALIDATE CONSTRAINT task_key;

-- +goose Down
DELETE FROM task_state WHERE key = 'sync_lists';
ALTER TABLE task_state DROP CONSTRAINT IF EXISTS task_key,
  ADD CONSTRAINT task_key CHECK (key IN ('scan_libraries', 'sweep_jobs', 'backup_database', 'refresh_metadata',
    'sweep_artwork', 'detect_markers', 'backfill_previews', 'sweep_downloads', 'prune_activity', 'refresh_collections'));
DROP INDEX CONCURRENTLY IF EXISTS external_ids_value;
DELETE FROM items WHERE id IN (SELECT item_id FROM collections WHERE origin = 'list');
ALTER TABLE collections DROP CONSTRAINT IF EXISTS collection_list,
  DROP COLUMN IF EXISTS list_source, DROP COLUMN IF EXISTS list_id, DROP COLUMN IF EXISTS list_missing;
ALTER TABLE collections DROP CONSTRAINT IF EXISTS collection_origin,
  ADD CONSTRAINT collection_origin CHECK (origin IN ('tmdb', 'user', 'smart'));
