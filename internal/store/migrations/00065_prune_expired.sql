-- +goose Up
ALTER TABLE task_state DROP CONSTRAINT task_key, ADD CONSTRAINT task_key CHECK (key IN ('scan_libraries',
  'sweep_jobs', 'backup_database', 'refresh_metadata', 'sweep_artwork', 'detect_markers',
  'backfill_previews', 'sweep_downloads', 'prune_activity', 'refresh_collections', 'sync_lists',
  'fetch_subtitles', 'refresh_anime_lists', 'prune_expired'));

-- +goose Down
DELETE FROM task_state WHERE key = 'prune_expired';
ALTER TABLE task_state DROP CONSTRAINT task_key, ADD CONSTRAINT task_key CHECK (key IN ('scan_libraries',
  'sweep_jobs', 'backup_database', 'refresh_metadata', 'sweep_artwork', 'detect_markers',
  'backfill_previews', 'sweep_downloads', 'prune_activity', 'refresh_collections', 'sync_lists',
  'fetch_subtitles', 'refresh_anime_lists'));
