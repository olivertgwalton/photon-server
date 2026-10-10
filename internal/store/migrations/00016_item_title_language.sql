-- +goose NO TRANSACTION
-- +goose Up
-- A film or show may give its original title, or its localized one, over what its library gives,
-- as Plex's per-item "Use original title" does; null is its library's.
ALTER TABLE items ADD COLUMN title_language text;
ALTER TABLE items ADD CONSTRAINT item_title_language CHECK (title_language IN ('localized', 'original')) NOT VALID;
ALTER TABLE items VALIDATE CONSTRAINT item_title_language;

-- +goose Down
ALTER TABLE items DROP COLUMN title_language;
