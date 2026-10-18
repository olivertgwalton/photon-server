-- +goose Up
-- Whether the server asks GitHub for a newer release of itself, as an admin sets it.
ALTER TABLE server ADD COLUMN release_check text NOT NULL DEFAULT 'on'
  CONSTRAINT release_check CHECK (release_check IN ('on', 'off'));

-- +goose Down
ALTER TABLE server DROP COLUMN release_check;
