-- +goose NO TRANSACTION
-- +goose Up
-- A music library holds artists, their albums and the albums' tracks, as Plex's holds them: a
-- track's disc and its place on it, and the artists the track names beside its album's. Each check
-- on items is checked again apart from its change, reading the table without stopping its writes.
ALTER TABLE libraries DROP CONSTRAINT library_kind,
  ADD CONSTRAINT library_kind CHECK (kind IN ('movies', 'shows', 'mixed', 'music'));
ALTER TABLE items DROP CONSTRAINT item_kind, ADD CONSTRAINT item_kind CHECK (kind IN ('movie', 'show', 'season',
  'episode', 'extra', 'collection', 'artist', 'album', 'track')) NOT VALID;
ALTER TABLE items VALIDATE CONSTRAINT item_kind;
ALTER TABLE items ADD COLUMN disc_number int, ADD COLUMN track_number int, ADD COLUMN artists jsonb;
ALTER TABLE library_sources DROP CONSTRAINT library_source_item_kind, ADD CONSTRAINT library_source_item_kind
  CHECK (item_kind IN ('movie', 'show', 'season', 'episode', 'artist', 'album', 'track'));
ALTER TABLE home_sections DROP CONSTRAINT home_row, ADD CONSTRAINT home_row CHECK (home_row IN ('continue_watching',
  'next_up', 'watchlist', 'favourites', 'recently_added_films', 'recently_added_shows', 'recently_added_albums',
  'recently_released', 'top_rated_unwatched'));

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

-- +goose Down
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION ranked_as(library_kind text, item_kind text) RETURNS text LANGUAGE sql IMMUTABLE PARALLEL SAFE RETURN
  CASE
    WHEN library_kind IN ('shows', 'mixed') AND item_kind IN ('show', 'season', 'episode') THEN item_kind
    WHEN library_kind = 'shows' THEN 'show'
    ELSE 'movie'
  END;
-- +goose StatementEnd

DELETE FROM libraries WHERE kind = 'music';
DELETE FROM home_sections WHERE home_row = 'recently_added_albums';
ALTER TABLE home_sections DROP CONSTRAINT home_row, ADD CONSTRAINT home_row CHECK (home_row IN ('continue_watching',
  'next_up', 'watchlist', 'favourites', 'recently_added_films', 'recently_added_shows', 'recently_released',
  'top_rated_unwatched'));
ALTER TABLE library_sources DROP CONSTRAINT library_source_item_kind, ADD CONSTRAINT library_source_item_kind
  CHECK (item_kind IN ('movie', 'show', 'season', 'episode'));
ALTER TABLE items DROP COLUMN disc_number, DROP COLUMN track_number, DROP COLUMN artists;
ALTER TABLE items DROP CONSTRAINT item_kind, ADD CONSTRAINT item_kind CHECK (kind IN ('movie', 'show', 'season',
  'episode', 'extra', 'collection')) NOT VALID;
ALTER TABLE items VALIDATE CONSTRAINT item_kind;
ALTER TABLE libraries DROP CONSTRAINT library_kind,
  ADD CONSTRAINT library_kind CHECK (kind IN ('movies', 'shows', 'mixed'));
