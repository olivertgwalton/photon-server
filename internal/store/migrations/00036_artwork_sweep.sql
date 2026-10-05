-- +goose Up
ALTER TABLE task_state DROP CONSTRAINT task_key,
  ADD CONSTRAINT task_key CHECK (key IN ('scan_libraries', 'sweep_jobs', 'backup_database', 'refresh_metadata', 'sweep_artwork'));

-- +goose Down
DELETE FROM task_state WHERE key = 'sweep_artwork';
ALTER TABLE task_state DROP CONSTRAINT task_key,
  ADD CONSTRAINT task_key CHECK (key IN ('scan_libraries', 'sweep_jobs', 'backup_database', 'refresh_metadata'));
