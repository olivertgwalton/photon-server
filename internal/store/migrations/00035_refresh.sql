-- +goose Up
-- How often a library's titles are matched again, in days (0 never), and when each title was last.
ALTER TABLE libraries ADD COLUMN refresh_days smallint NOT NULL DEFAULT 30 CHECK (refresh_days BETWEEN 0 AND 365);
ALTER TABLE items ADD COLUMN identified_at timestamptz;
ALTER TABLE task_state DROP CONSTRAINT task_key,
  ADD CONSTRAINT task_key CHECK (key IN ('scan_libraries', 'sweep_jobs', 'backup_database', 'refresh_metadata'));

-- +goose Down
DELETE FROM task_state WHERE key = 'refresh_metadata';
ALTER TABLE task_state DROP CONSTRAINT task_key,
  ADD CONSTRAINT task_key CHECK (key IN ('scan_libraries', 'sweep_jobs', 'backup_database'));
ALTER TABLE items DROP COLUMN identified_at;
ALTER TABLE libraries DROP COLUMN refresh_days;
