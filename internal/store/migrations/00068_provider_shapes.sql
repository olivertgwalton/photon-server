-- +goose NO TRANSACTION
-- +goose Up
-- A source or provider a plugin may fill is checked for its shape alone: a built-in's name, or a
-- plugin's. Which names there are is Go's to say, as a plugin's always was, so adding a provider
-- changes no check and reads no table again. Each check is checked once more apart from its
-- change, reading the table without stopping its writes.
ALTER TABLE item_fields DROP CONSTRAINT field_source,
  ADD CONSTRAINT field_source CHECK (source ~ '^(plugin:)?[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE item_fields VALIDATE CONSTRAINT field_source;
ALTER TABLE artwork DROP CONSTRAINT artwork_source,
  ADD CONSTRAINT artwork_source CHECK (source ~ '^(plugin:)?[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE artwork VALIDATE CONSTRAINT artwork_source;
ALTER TABLE ratings DROP CONSTRAINT rating_source,
  ADD CONSTRAINT rating_source CHECK (source ~ '^(plugin:)?[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE ratings VALIDATE CONSTRAINT rating_source;
ALTER TABLE credits DROP CONSTRAINT credit_source,
  ADD CONSTRAINT credit_source CHECK (source ~ '^(plugin:)?[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE credits VALIDATE CONSTRAINT credit_source;
ALTER TABLE external_ids DROP CONSTRAINT id_provider,
  ADD CONSTRAINT id_provider CHECK (provider ~ '^(plugin:)?[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE external_ids VALIDATE CONSTRAINT id_provider;
ALTER TABLE person_ids DROP CONSTRAINT person_id_provider,
  ADD CONSTRAINT person_id_provider CHECK (provider ~ '^(plugin:)?[a-z0-9][a-z0-9-]{0,62}$') NOT
  VALID;
ALTER TABLE person_ids VALIDATE CONSTRAINT person_id_provider;
ALTER TABLE library_sources DROP CONSTRAINT library_source,
  ADD CONSTRAINT library_source CHECK (source ~ '^(plugin:)?[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE library_sources VALIDATE CONSTRAINT library_source;
ALTER TABLE remote_videos DROP CONSTRAINT remote_video_source,
  ADD CONSTRAINT remote_video_source CHECK (source ~ '^(plugin:)?[a-z0-9][a-z0-9-]{0,62}$') NOT
  VALID;
ALTER TABLE remote_videos VALIDATE CONSTRAINT remote_video_source;
ALTER TABLE announced_episodes DROP CONSTRAINT announced_source,
  ADD CONSTRAINT announced_source CHECK (source ~ '^(plugin:)?[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE announced_episodes VALIDATE CONSTRAINT announced_source;
ALTER TABLE discoveries DROP CONSTRAINT discovery_provider,
  ADD CONSTRAINT discovery_provider CHECK (provider ~ '^(plugin:)?[a-z0-9][a-z0-9-]{0,62}$') NOT
  VALID;
ALTER TABLE discoveries VALIDATE CONSTRAINT discovery_provider;
ALTER TABLE requests DROP CONSTRAINT request_provider,
  ADD CONSTRAINT request_provider CHECK (provider ~ '^(plugin:)?[a-z0-9][a-z0-9-]{0,62}$') NOT
  VALID;
ALTER TABLE requests VALIDATE CONSTRAINT request_provider;

-- +goose Down
ALTER TABLE item_fields DROP CONSTRAINT field_source,
  ADD CONSTRAINT field_source CHECK (source IN ('file', 'tmdb', 'tvdb', 'nfo', 'user', 'mdblist',
  'omdb', 'opensubtitles', 'anilist', 'anidb', 'fanart', 'riven', 'radarr', 'sonarr') OR source ~
  '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE item_fields VALIDATE CONSTRAINT field_source;
ALTER TABLE artwork DROP CONSTRAINT artwork_source,
  ADD CONSTRAINT artwork_source CHECK (source IN ('file', 'tmdb', 'tvdb', 'user', 'omdb', 'anilist',
  'anidb', 'fanart') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE artwork VALIDATE CONSTRAINT artwork_source;
ALTER TABLE ratings DROP CONSTRAINT rating_source,
  ADD CONSTRAINT rating_source CHECK (source IN ('nfo', 'tmdb', 'tvdb', 'mdblist', 'omdb',
  'anilist', 'anidb') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE ratings VALIDATE CONSTRAINT rating_source;
ALTER TABLE credits DROP CONSTRAINT credit_source,
  ADD CONSTRAINT credit_source CHECK (source IN ('nfo', 'tmdb', 'tvdb', 'anilist', 'anidb') OR
  source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE credits VALIDATE CONSTRAINT credit_source;
ALTER TABLE external_ids DROP CONSTRAINT id_provider,
  ADD CONSTRAINT id_provider CHECK (provider IN ('tmdb', 'imdb', 'tvdb', 'anidb', 'anilist') OR
  provider ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE external_ids VALIDATE CONSTRAINT id_provider;
ALTER TABLE person_ids DROP CONSTRAINT person_id_provider,
  ADD CONSTRAINT person_id_provider CHECK (provider IN ('tmdb', 'imdb', 'tvdb', 'anidb', 'anilist')
  OR provider ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE person_ids VALIDATE CONSTRAINT person_id_provider;
ALTER TABLE library_sources DROP CONSTRAINT library_source,
  ADD CONSTRAINT library_source CHECK (source IN ('nfo', 'tmdb', 'tvdb', 'mdblist', 'omdb',
  'anilist', 'anidb', 'fanart') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE library_sources VALIDATE CONSTRAINT library_source;
ALTER TABLE remote_videos DROP CONSTRAINT remote_video_source,
  ADD CONSTRAINT remote_video_source CHECK (source IN ('tmdb', 'tvdb') OR source ~
  '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE remote_videos VALIDATE CONSTRAINT remote_video_source;
ALTER TABLE announced_episodes DROP CONSTRAINT announced_source,
  ADD CONSTRAINT announced_source CHECK (source IN ('tmdb', 'tvdb') OR source ~
  '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE announced_episodes VALIDATE CONSTRAINT announced_source;
ALTER TABLE discoveries DROP CONSTRAINT discovery_provider,
  ADD CONSTRAINT discovery_provider CHECK (provider IN ('tmdb', 'imdb', 'tvdb', 'anidb', 'anilist')
  OR provider ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE discoveries VALIDATE CONSTRAINT discovery_provider;
ALTER TABLE requests DROP CONSTRAINT request_provider,
  ADD CONSTRAINT request_provider CHECK (provider IN ('tmdb', 'imdb', 'tvdb', 'anidb', 'anilist') OR
  provider ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE requests VALIDATE CONSTRAINT request_provider;
