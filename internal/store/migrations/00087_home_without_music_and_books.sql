-- +goose Up
-- Home is films and television: music and books are reached by their own libraries and the
-- profile's favourites, so Continue Reading and the rows of what was added to a music or a books
-- library are no longer rows. The table is small, a row per profile's home row.
DELETE FROM home_sections WHERE home_row IN ('continue_reading', 'recently_added_albums', 'recently_added_books');
ALTER TABLE home_sections DROP CONSTRAINT home_row, ADD CONSTRAINT home_row CHECK (home_row IN ('continue_watching',
  'next_up', 'watchlist', 'favourites', 'recently_added_films', 'recently_added_shows', 'recently_released',
  'top_rated_unwatched'));

-- +goose Down
ALTER TABLE home_sections DROP CONSTRAINT home_row, ADD CONSTRAINT home_row CHECK (home_row IN ('continue_watching',
  'continue_reading', 'next_up', 'watchlist', 'favourites', 'recently_added_films', 'recently_added_shows',
  'recently_added_albums', 'recently_added_books', 'recently_released', 'top_rated_unwatched'));
