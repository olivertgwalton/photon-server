-- +goose Up
-- The activity log: what happened, when, and the profile, title and library it was about while
-- they last. Removing one leaves its entries, as Jellyfin's log keeps a deleted user's sign-ins.
CREATE TABLE activity (
  id uuid PRIMARY KEY DEFAULT uuidv7(),
  at timestamptz NOT NULL DEFAULT now(),
  kind text NOT NULL CONSTRAINT activity_kind CHECK (kind IN (
    'playback.started', 'playback.stopped', 'auth.signed_in', 'auth.sign_in_refused',
    'profile.added', 'profile.removed', 'library.added', 'library.removed', 'library.scanned',
    'library.titles_added', 'task.failed', 'backup.made', 'job.dead')),
  profile_id uuid REFERENCES profiles(id) ON DELETE SET NULL,
  item_id uuid REFERENCES items(id) ON DELETE SET NULL,
  library_id uuid REFERENCES libraries(id) ON DELETE SET NULL,
  details jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX activity_at ON activity (at DESC, id DESC);
CREATE INDEX activity_kind_at ON activity (kind, at DESC, id DESC);

ALTER TABLE task_state DROP CONSTRAINT task_key,
  ADD CONSTRAINT task_key CHECK (key IN ('scan_libraries', 'sweep_jobs', 'backup_database', 'refresh_metadata', 'sweep_artwork', 'detect_markers', 'backfill_previews', 'sweep_downloads', 'prune_activity'));

-- +goose Down
DELETE FROM task_state WHERE key = 'prune_activity';
ALTER TABLE task_state DROP CONSTRAINT task_key,
  ADD CONSTRAINT task_key CHECK (key IN ('scan_libraries', 'sweep_jobs', 'backup_database', 'refresh_metadata', 'sweep_artwork', 'detect_markers', 'backfill_previews', 'sweep_downloads'));
DROP TABLE activity;
