-- +goose NO TRANSACTION
-- +goose Up
-- fanart.tv pictures films and shows. Each check is checked again apart from its change, reading
-- the table without stopping its writes.
ALTER TABLE item_fields DROP CONSTRAINT field_source, ADD CONSTRAINT field_source CHECK (source IN ('file',
  'tmdb', 'tvdb', 'nfo', 'user', 'mdblist', 'omdb', 'opensubtitles', 'anilist', 'anidb', 'fanart')
  OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE item_fields VALIDATE CONSTRAINT field_source;
ALTER TABLE library_sources DROP CONSTRAINT library_source, ADD CONSTRAINT library_source CHECK (source IN ('nfo',
  'tmdb', 'tvdb', 'mdblist', 'omdb', 'anilist', 'anidb', 'fanart') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$');
ALTER TABLE artwork DROP CONSTRAINT artwork_source, ADD CONSTRAINT artwork_source CHECK (source IN ('file',
  'tmdb', 'tvdb', 'user', 'omdb', 'anilist', 'anidb', 'fanart') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE artwork VALIDATE CONSTRAINT artwork_source;

-- +goose Down
DELETE FROM artwork WHERE source = 'fanart';
ALTER TABLE artwork DROP CONSTRAINT artwork_source, ADD CONSTRAINT artwork_source CHECK (source IN ('file',
  'tmdb', 'tvdb', 'user', 'omdb', 'anilist', 'anidb') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$');
DELETE FROM library_sources WHERE source = 'fanart';
ALTER TABLE library_sources DROP CONSTRAINT library_source, ADD CONSTRAINT library_source CHECK (source IN ('nfo',
  'tmdb', 'tvdb', 'mdblist', 'omdb', 'anilist', 'anidb') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$');
DELETE FROM item_fields WHERE source = 'fanart';
ALTER TABLE item_fields DROP CONSTRAINT field_source, ADD CONSTRAINT field_source CHECK (source IN ('file',
  'tmdb', 'tvdb', 'nfo', 'user', 'mdblist', 'omdb', 'opensubtitles', 'anilist', 'anidb')
  OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$');
