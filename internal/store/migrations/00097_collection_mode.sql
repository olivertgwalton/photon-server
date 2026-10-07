-- +goose Up
-- How a library's wall shows its collections, as Plex's "Collections" setting: grouped, in place
-- of the titles they hold, as Jellyfin's grouping into collections; shown beside them; or hidden.
ALTER TABLE libraries ADD COLUMN collection_mode text NOT NULL DEFAULT 'grouped'
  CONSTRAINT collection_mode CHECK (collection_mode IN ('grouped', 'shown', 'hidden'));

-- +goose Down
ALTER TABLE libraries DROP COLUMN collection_mode;
