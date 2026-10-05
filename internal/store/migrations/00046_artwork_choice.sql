-- +goose Up
-- An admin's choice of a title's picture of a kind: a copy of a provider's, which outranks every
-- source and outlasts what the provider says next.
ALTER TABLE artwork DROP CONSTRAINT artwork_source, ADD CONSTRAINT artwork_source CHECK (
  source IN ('file', 'tmdb', 'tvdb', 'user') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$');
CREATE UNIQUE INDEX artwork_chosen ON artwork (item_id, kind) WHERE source = 'user';

-- +goose Down
DROP INDEX artwork_chosen;
DELETE FROM artwork WHERE source = 'user';
ALTER TABLE artwork DROP CONSTRAINT artwork_source, ADD CONSTRAINT artwork_source CHECK (
  source IN ('file', 'tmdb', 'tvdb') OR source ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$');
