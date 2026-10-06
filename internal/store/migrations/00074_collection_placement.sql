-- +goose NO TRANSACTION
-- +goose Up
-- Where a collection is shown: in its library, or on the home page as well, as Plex's Promote to
-- Home.
ALTER TABLE collections ADD COLUMN placement text NOT NULL DEFAULT 'library';
ALTER TABLE collections ADD CONSTRAINT collection_placement CHECK (placement IN ('library', 'home')) NOT VALID;
ALTER TABLE collections VALIDATE CONSTRAINT collection_placement;

-- A profile places the rows of the collections on its home as it does any other row.
ALTER TABLE home_sections DROP CONSTRAINT home_row, ADD CONSTRAINT home_row CHECK (home_row IN (
  'continue_watching', 'next_up', 'favourites', 'recently_added_films', 'recently_added_shows', 'collection')) NOT VALID;
ALTER TABLE home_sections VALIDATE CONSTRAINT home_row;

-- +goose Down
DELETE FROM home_sections WHERE home_row = 'collection';
ALTER TABLE home_sections DROP CONSTRAINT home_row, ADD CONSTRAINT home_row CHECK (home_row IN (
  'continue_watching', 'next_up', 'favourites', 'recently_added_films', 'recently_added_shows')) NOT VALID;
ALTER TABLE home_sections VALIDATE CONSTRAINT home_row;
ALTER TABLE collections DROP COLUMN placement;
