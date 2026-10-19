-- +goose Up
-- A collection says how a library's wall shows it, as Plex's collection mode: as the library
-- shows its box sets, or collapsed, expanded or beside its titles whatever it does.
ALTER TABLE collections ADD COLUMN box_set text NOT NULL DEFAULT 'library'
  CONSTRAINT collection_box_set CHECK (box_set IN ('library', 'collapsed', 'expanded', 'beside'));

-- +goose Down
ALTER TABLE collections DROP COLUMN box_set;
