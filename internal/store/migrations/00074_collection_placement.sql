-- +goose NO TRANSACTION
-- +goose Up
-- Where a collection is shown: in its library, or on the home page as well, as Plex's Promote to
-- Home.
ALTER TABLE collections ADD COLUMN placement text NOT NULL DEFAULT 'library';
ALTER TABLE collections ADD CONSTRAINT collection_placement CHECK (placement IN ('library', 'home')) NOT VALID;
ALTER TABLE collections VALIDATE CONSTRAINT collection_placement;

-- +goose Down
ALTER TABLE collections DROP COLUMN placement;
