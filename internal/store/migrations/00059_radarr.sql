-- +goose NO TRANSACTION
-- +goose Up
-- Radarr fetches the films a library lacks that its profiles ask for. The check is checked again
-- apart from its change, reading the table without stopping its writes.
ALTER TABLE item_fields DROP CONSTRAINT field_source, ADD CONSTRAINT field_source CHECK (source IN ('file',
  'tmdb', 'tvdb', 'nfo', 'user', 'mdblist', 'omdb', 'opensubtitles', 'anilist', 'anidb', 'fanart', 'riven', 'radarr')
  OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE item_fields VALIDATE CONSTRAINT field_source;

-- +goose Down
DELETE FROM providers WHERE id = 'radarr';
UPDATE libraries SET request_source = NULL WHERE request_source = 'radarr';
ALTER TABLE item_fields DROP CONSTRAINT field_source, ADD CONSTRAINT field_source CHECK (source IN ('file',
  'tmdb', 'tvdb', 'nfo', 'user', 'mdblist', 'omdb', 'opensubtitles', 'anilist', 'anidb', 'fanart', 'riven')
  OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$');
