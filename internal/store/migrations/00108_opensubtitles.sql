-- +goose NO TRANSACTION
-- +goose Up
-- OpenSubtitles is a provider, and providers are field sources, though it gives no field.
ALTER TABLE item_fields DROP CONSTRAINT field_source, ADD CONSTRAINT field_source CHECK (
  source IN ('file', 'tmdb', 'tvdb', 'nfo', 'user', 'mdblist', 'omdb', 'opensubtitles')
  OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE item_fields VALIDATE CONSTRAINT field_source;

-- +goose Down
DELETE FROM providers WHERE id = 'opensubtitles';
ALTER TABLE item_fields DROP CONSTRAINT field_source, ADD CONSTRAINT field_source CHECK (
  source IN ('file', 'tmdb', 'tvdb', 'nfo', 'user', 'mdblist', 'omdb') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE item_fields VALIDATE CONSTRAINT field_source;
