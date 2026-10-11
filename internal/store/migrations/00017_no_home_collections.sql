-- +goose Up
-- A collection is no longer put on the home page: nothing could put one there. Both tables are
-- small, a row per profile's home row and one per collection.
DELETE FROM home_sections WHERE home_row = 'collection';
ALTER TABLE home_sections DROP CONSTRAINT home_row;
ALTER TABLE home_sections ADD CONSTRAINT home_row CHECK (home_row IN ('continue_watching', 'next_up',
  'watchlist', 'favourites', 'recently_added_films', 'recently_added_shows', 'recently_released',
  'top_rated_unwatched'));
ALTER TABLE collections DROP COLUMN placement;

-- +goose Down
ALTER TABLE collections ADD COLUMN placement text NOT NULL DEFAULT 'library'
  CONSTRAINT collection_placement CHECK (placement IN ('library', 'home'));
ALTER TABLE home_sections DROP CONSTRAINT home_row;
ALTER TABLE home_sections ADD CONSTRAINT home_row CHECK (home_row IN ('continue_watching', 'next_up',
  'watchlist', 'favourites', 'recently_added_films', 'recently_added_shows', 'recently_released',
  'top_rated_unwatched', 'collection'));
