-- +goose NO TRANSACTION
-- +goose Up
-- OMDb describes titles by their IMDb ids, rates them and gives a poster, so a library may list it
-- among its sources.
ALTER TABLE item_fields DROP CONSTRAINT field_source, ADD CONSTRAINT field_source CHECK (
  source IN ('file', 'tmdb', 'tvdb', 'nfo', 'user', 'mdblist', 'omdb') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE item_fields VALIDATE CONSTRAINT field_source;
ALTER TABLE library_sources DROP CONSTRAINT library_source, ADD CONSTRAINT library_source CHECK (
  source IN ('nfo', 'tmdb', 'tvdb', 'mdblist', 'omdb') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE library_sources VALIDATE CONSTRAINT library_source;
ALTER TABLE ratings DROP CONSTRAINT rating_source, ADD CONSTRAINT rating_source CHECK (
  source IN ('nfo', 'tmdb', 'tvdb', 'mdblist', 'omdb') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE ratings VALIDATE CONSTRAINT rating_source;
ALTER TABLE artwork DROP CONSTRAINT artwork_source, ADD CONSTRAINT artwork_source CHECK (
  source IN ('file', 'tmdb', 'tvdb', 'user', 'omdb') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE artwork VALIDATE CONSTRAINT artwork_source;

-- +goose Down
DELETE FROM artwork WHERE source = 'omdb';
DELETE FROM ratings WHERE source = 'omdb';
DELETE FROM library_sources WHERE source = 'omdb';
DELETE FROM item_fields WHERE source = 'omdb';
DELETE FROM providers WHERE id = 'omdb';
ALTER TABLE artwork DROP CONSTRAINT artwork_source, ADD CONSTRAINT artwork_source CHECK (
  source IN ('file', 'tmdb', 'tvdb', 'user') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE artwork VALIDATE CONSTRAINT artwork_source;
ALTER TABLE ratings DROP CONSTRAINT rating_source, ADD CONSTRAINT rating_source CHECK (
  source IN ('nfo', 'tmdb', 'tvdb', 'mdblist') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE ratings VALIDATE CONSTRAINT rating_source;
ALTER TABLE library_sources DROP CONSTRAINT library_source, ADD CONSTRAINT library_source CHECK (
  source IN ('nfo', 'tmdb', 'tvdb', 'mdblist') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE library_sources VALIDATE CONSTRAINT library_source;
ALTER TABLE item_fields DROP CONSTRAINT field_source, ADD CONSTRAINT field_source CHECK (
  source IN ('file', 'tmdb', 'tvdb', 'nfo', 'user', 'mdblist') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE item_fields VALIDATE CONSTRAINT field_source;
