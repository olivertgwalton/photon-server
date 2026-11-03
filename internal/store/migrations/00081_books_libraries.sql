-- +goose NO TRANSACTION
-- +goose Up
-- A books library holds authors and their books, as a music library holds artists and their
-- albums: a book's versions are its formats, an ebook's file or an audiobook's, and it keeps the
-- series it is in and its place there. Where a reader is in a book is its reader's locator and how
-- far through it that is. Each check on a large table is checked again apart from its change,
-- reading the table without stopping its writes.
ALTER TABLE libraries DROP CONSTRAINT library_kind,
  ADD CONSTRAINT library_kind CHECK (kind IN ('movies', 'shows', 'mixed', 'music', 'books'));
ALTER TABLE items DROP CONSTRAINT item_kind, ADD CONSTRAINT item_kind CHECK (kind IN ('movie', 'show', 'season',
  'episode', 'extra', 'collection', 'artist', 'album', 'track', 'author', 'book')) NOT VALID;
ALTER TABLE items VALIDATE CONSTRAINT item_kind;
ALTER TABLE items ADD COLUMN series text, ADD COLUMN series_index numeric;
ALTER TABLE library_sources DROP CONSTRAINT library_source_item_kind, ADD CONSTRAINT library_source_item_kind
  CHECK (item_kind IN ('movie', 'show', 'season', 'episode', 'artist', 'album', 'track', 'author', 'book'));
ALTER TABLE home_sections DROP CONSTRAINT home_row, ADD CONSTRAINT home_row CHECK (home_row IN ('continue_watching',
  'continue_reading', 'next_up', 'watchlist', 'favourites', 'recently_added_films', 'recently_added_shows',
  'recently_added_albums', 'recently_added_books', 'recently_released', 'top_rated_unwatched'));
ALTER TABLE watch_state ADD COLUMN locator jsonb, ADD COLUMN progression real;
ALTER TABLE watch_state ADD CONSTRAINT progression_fraction CHECK (progression BETWEEN 0 AND 1) NOT VALID;
ALTER TABLE watch_state VALIDATE CONSTRAINT progression_fraction;
CREATE INDEX CONCURRENTLY IF NOT EXISTS watch_state_reading ON watch_state (profile_id, last_played_at DESC)
  WHERE progression > 0 AND progression < 1;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ranked_as(library_kind text, item_kind text) RETURNS text LANGUAGE sql IMMUTABLE PARALLEL SAFE RETURN
  CASE
    WHEN library_kind IN ('shows', 'mixed') AND item_kind IN ('show', 'season', 'episode') THEN item_kind
    WHEN library_kind = 'shows' THEN 'show'
    WHEN library_kind = 'music' AND item_kind IN ('artist', 'album', 'track') THEN item_kind
    WHEN library_kind = 'music' THEN 'artist'
    WHEN library_kind = 'books' AND item_kind IN ('author', 'book') THEN item_kind
    WHEN library_kind = 'books' THEN 'author'
    ELSE 'movie'
  END;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ranked_as(library_kind text, item_kind text) RETURNS text LANGUAGE sql IMMUTABLE PARALLEL SAFE RETURN
  CASE
    WHEN library_kind IN ('shows', 'mixed') AND item_kind IN ('show', 'season', 'episode') THEN item_kind
    WHEN library_kind = 'shows' THEN 'show'
    WHEN library_kind = 'music' AND item_kind IN ('artist', 'album', 'track') THEN item_kind
    WHEN library_kind = 'music' THEN 'artist'
    ELSE 'movie'
  END;
-- +goose StatementEnd

DROP INDEX CONCURRENTLY IF EXISTS watch_state_reading;
ALTER TABLE watch_state DROP COLUMN locator, DROP COLUMN progression;
DELETE FROM libraries WHERE kind = 'books';
DELETE FROM home_sections WHERE home_row IN ('continue_reading', 'recently_added_books');
ALTER TABLE home_sections DROP CONSTRAINT home_row, ADD CONSTRAINT home_row CHECK (home_row IN ('continue_watching',
  'next_up', 'watchlist', 'favourites', 'recently_added_films', 'recently_added_shows', 'recently_added_albums',
  'recently_released', 'top_rated_unwatched'));
ALTER TABLE library_sources DROP CONSTRAINT library_source_item_kind, ADD CONSTRAINT library_source_item_kind
  CHECK (item_kind IN ('movie', 'show', 'season', 'episode', 'artist', 'album', 'track'));
ALTER TABLE items DROP COLUMN series, DROP COLUMN series_index;
ALTER TABLE items DROP CONSTRAINT item_kind, ADD CONSTRAINT item_kind CHECK (kind IN ('movie', 'show', 'season',
  'episode', 'extra', 'collection', 'artist', 'album', 'track')) NOT VALID;
ALTER TABLE items VALIDATE CONSTRAINT item_kind;
ALTER TABLE libraries DROP CONSTRAINT library_kind,
  ADD CONSTRAINT library_kind CHECK (kind IN ('movies', 'shows', 'mixed', 'music'));
