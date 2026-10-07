-- +goose Up
-- A subtitle fetched from a provider is kept here, its text with it, as Plex keeps those it
-- fetches: a library may be read-only, and every node serves it. Nullable with no default, so
-- adding it rewrites nothing.
ALTER TABLE subtitle_files ADD COLUMN body bytea;

-- +goose Down
DELETE FROM subtitle_files WHERE body IS NOT NULL;
ALTER TABLE subtitle_files DROP COLUMN body;
