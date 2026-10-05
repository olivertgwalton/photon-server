-- +goose Up
-- A metadata plugin: a web service an admin registered, and the manifest it last answered. Its
-- source id is plugin:<slug>, so what it says is kept beside the built-in sources' and a title
-- can carry its id.
CREATE TABLE plugins (
  slug text PRIMARY KEY CHECK (('plugin:' || slug) ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$'),
  url text NOT NULL,
  manifest jsonb NOT NULL
);

ALTER TABLE item_fields DROP CONSTRAINT field_source, ADD CONSTRAINT field_source CHECK (
  source IN ('file', 'tmdb', 'tvdb', 'nfo', 'user', 'mdblist') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$');
ALTER TABLE library_sources DROP CONSTRAINT library_source, ADD CONSTRAINT library_source CHECK (
  source IN ('nfo', 'tmdb', 'tvdb', 'mdblist') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$');
ALTER TABLE ratings DROP CONSTRAINT rating_source, ADD CONSTRAINT rating_source CHECK (
  source IN ('nfo', 'tmdb', 'tvdb', 'mdblist') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$');
ALTER TABLE credits DROP CONSTRAINT credit_source, ADD CONSTRAINT credit_source CHECK (
  source IN ('nfo', 'tmdb', 'tvdb') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$');
ALTER TABLE artwork DROP CONSTRAINT artwork_source, ADD CONSTRAINT artwork_source CHECK (
  source IN ('file', 'tmdb', 'tvdb') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$');
ALTER TABLE remote_videos DROP CONSTRAINT remote_video_source, ADD CONSTRAINT remote_video_source CHECK (
  source IN ('tmdb', 'tvdb') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$');
ALTER TABLE external_ids DROP CONSTRAINT id_provider, ADD CONSTRAINT id_provider CHECK (
  provider IN ('tmdb', 'imdb', 'tvdb') OR provider ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$');

-- +goose Down
DELETE FROM external_ids WHERE provider LIKE 'plugin:%';
DELETE FROM remote_videos WHERE source LIKE 'plugin:%';
DELETE FROM artwork WHERE source LIKE 'plugin:%';
DELETE FROM credits WHERE source LIKE 'plugin:%';
DELETE FROM ratings WHERE source LIKE 'plugin:%';
DELETE FROM library_sources WHERE source LIKE 'plugin:%';
DELETE FROM item_fields WHERE source LIKE 'plugin:%';
DELETE FROM providers WHERE id LIKE 'plugin:%';
ALTER TABLE external_ids DROP CONSTRAINT id_provider,
  ADD CONSTRAINT id_provider CHECK (provider IN ('tmdb', 'imdb', 'tvdb'));
ALTER TABLE remote_videos DROP CONSTRAINT remote_video_source,
  ADD CONSTRAINT remote_video_source CHECK (source IN ('tmdb', 'tvdb'));
ALTER TABLE artwork DROP CONSTRAINT artwork_source,
  ADD CONSTRAINT artwork_source CHECK (source IN ('file', 'tmdb', 'tvdb'));
ALTER TABLE credits DROP CONSTRAINT credit_source,
  ADD CONSTRAINT credit_source CHECK (source IN ('nfo', 'tmdb', 'tvdb'));
ALTER TABLE ratings DROP CONSTRAINT rating_source,
  ADD CONSTRAINT rating_source CHECK (source IN ('nfo', 'tmdb', 'tvdb', 'mdblist'));
ALTER TABLE library_sources DROP CONSTRAINT library_source,
  ADD CONSTRAINT library_source CHECK (source IN ('nfo', 'tmdb', 'tvdb', 'mdblist'));
ALTER TABLE item_fields DROP CONSTRAINT field_source,
  ADD CONSTRAINT field_source CHECK (source IN ('file', 'tmdb', 'tvdb', 'nfo', 'user', 'mdblist'));
DROP TABLE plugins;
