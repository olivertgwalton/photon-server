-- +goose NO TRANSACTION
-- +goose Up
-- A home may show what was released lately.
ALTER TABLE home_sections DROP CONSTRAINT home_row, ADD CONSTRAINT home_row CHECK (home_row IN (
  'continue_watching', 'next_up', 'favourites', 'recently_added_films', 'recently_added_shows', 'recently_released', 'collection')) NOT VALID;
ALTER TABLE home_sections VALIDATE CONSTRAINT home_row;

-- +goose Down
DELETE FROM home_sections WHERE home_row IN ('recently_released');
ALTER TABLE home_sections DROP CONSTRAINT home_row, ADD CONSTRAINT home_row CHECK (home_row IN (
  'continue_watching', 'next_up', 'favourites', 'recently_added_films', 'recently_added_shows', 'collection')) NOT VALID;
ALTER TABLE home_sections VALIDATE CONSTRAINT home_row;
