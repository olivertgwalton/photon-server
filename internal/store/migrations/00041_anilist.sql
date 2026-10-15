-- +goose NO TRANSACTION
-- +goose Up
-- AniList describes, pictures, credits and rates titles, and is a site rated on. Each check is
-- checked again apart from its change, reading the table without stopping its writes.
ALTER TABLE item_fields DROP CONSTRAINT field_source, ADD CONSTRAINT field_source CHECK (source IN ('file',
  'tmdb', 'tvdb', 'nfo', 'user', 'mdblist', 'omdb', 'opensubtitles', 'anilist') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE item_fields VALIDATE CONSTRAINT field_source;
ALTER TABLE library_sources DROP CONSTRAINT library_source, ADD CONSTRAINT library_source CHECK (source IN ('nfo',
  'tmdb', 'tvdb', 'mdblist', 'omdb', 'anilist') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$');
ALTER TABLE artwork DROP CONSTRAINT artwork_source, ADD CONSTRAINT artwork_source CHECK (source IN ('file',
  'tmdb', 'tvdb', 'user', 'omdb', 'anilist') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE artwork VALIDATE CONSTRAINT artwork_source;
ALTER TABLE ratings DROP CONSTRAINT rating_source, ADD CONSTRAINT rating_source CHECK (source IN ('nfo',
  'tmdb', 'tvdb', 'mdblist', 'omdb', 'anilist') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID,
  DROP CONSTRAINT rating_site, ADD CONSTRAINT rating_site CHECK (site IN ('imdb', 'tmdb', 'rotten_tomatoes',
  'rotten_tomatoes_audience', 'anilist')) NOT VALID;
ALTER TABLE ratings VALIDATE CONSTRAINT rating_source;
ALTER TABLE ratings VALIDATE CONSTRAINT rating_site;
ALTER TABLE credits DROP CONSTRAINT credit_source, ADD CONSTRAINT credit_source CHECK (source IN ('nfo',
  'tmdb', 'tvdb', 'anilist') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE credits VALIDATE CONSTRAINT credit_source;

-- +goose Down
DELETE FROM credits WHERE source = 'anilist';
ALTER TABLE credits DROP CONSTRAINT credit_source, ADD CONSTRAINT credit_source CHECK (source IN ('nfo',
  'tmdb', 'tvdb') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$');
DELETE FROM ratings WHERE source = 'anilist' OR site = 'anilist';
ALTER TABLE ratings DROP CONSTRAINT rating_source, ADD CONSTRAINT rating_source CHECK (source IN ('nfo',
  'tmdb', 'tvdb', 'mdblist', 'omdb') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$'),
  DROP CONSTRAINT rating_site, ADD CONSTRAINT rating_site CHECK (site IN ('imdb', 'tmdb', 'rotten_tomatoes',
  'rotten_tomatoes_audience'));
DELETE FROM artwork WHERE source = 'anilist';
ALTER TABLE artwork DROP CONSTRAINT artwork_source, ADD CONSTRAINT artwork_source CHECK (source IN ('file',
  'tmdb', 'tvdb', 'user', 'omdb') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$');
DELETE FROM library_sources WHERE source = 'anilist';
ALTER TABLE library_sources DROP CONSTRAINT library_source, ADD CONSTRAINT library_source CHECK (source IN ('nfo',
  'tmdb', 'tvdb', 'mdblist', 'omdb') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$');
DELETE FROM item_fields WHERE source = 'anilist';
ALTER TABLE item_fields DROP CONSTRAINT field_source, ADD CONSTRAINT field_source CHECK (source IN ('file',
  'tmdb', 'tvdb', 'nfo', 'user', 'mdblist', 'omdb', 'opensubtitles') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$');
