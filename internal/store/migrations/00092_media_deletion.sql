-- +goose Up
-- Whether an admin may delete a library's titles with their files, as Plex's "Allow media
-- deletion" and Jellyfin's per-folder deletion; off until an admin turns it on.
ALTER TABLE libraries ADD COLUMN deletion text NOT NULL DEFAULT 'off'
  CONSTRAINT media_deletion CHECK (deletion IN ('off', 'files'));

-- +goose Down
ALTER TABLE libraries DROP COLUMN deletion;
