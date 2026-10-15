-- +goose NO TRANSACTION
-- +goose Up
-- Titles and people carry AniDB and AniList ids too. Each check is checked again apart from its
-- change, reading the table without stopping its writes.
ALTER TABLE external_ids DROP CONSTRAINT id_provider, ADD CONSTRAINT id_provider CHECK (provider IN ('tmdb',
  'imdb', 'tvdb', 'anidb', 'anilist') OR provider ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE external_ids VALIDATE CONSTRAINT id_provider;
ALTER TABLE person_ids DROP CONSTRAINT person_id_provider, ADD CONSTRAINT person_id_provider
  CHECK (provider IN ('tmdb', 'imdb', 'tvdb', 'anidb', 'anilist') OR provider ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$') NOT VALID;
ALTER TABLE person_ids VALIDATE CONSTRAINT person_id_provider;
ALTER TABLE discoveries DROP CONSTRAINT discovery_provider, ADD CONSTRAINT discovery_provider
  CHECK (provider IN ('tmdb', 'imdb', 'tvdb', 'anidb', 'anilist') OR provider ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$');

-- +goose Down
DELETE FROM discoveries WHERE provider IN ('anidb', 'anilist');
ALTER TABLE discoveries DROP CONSTRAINT discovery_provider, ADD CONSTRAINT discovery_provider
  CHECK (provider IN ('tmdb', 'imdb', 'tvdb') OR provider ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$');
DELETE FROM person_ids WHERE provider IN ('anidb', 'anilist');
ALTER TABLE person_ids DROP CONSTRAINT person_id_provider, ADD CONSTRAINT person_id_provider
  CHECK (provider IN ('tmdb', 'imdb', 'tvdb') OR provider ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$');
DELETE FROM external_ids WHERE provider IN ('anidb', 'anilist');
ALTER TABLE external_ids DROP CONSTRAINT id_provider, ADD CONSTRAINT id_provider
  CHECK (provider IN ('tmdb', 'imdb', 'tvdb') OR provider ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$');
