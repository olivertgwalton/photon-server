-- +goose Up
-- A person is known by any provider's ids, as a title is, so someone a plugin or TheTVDB credits
-- is kept whether or not TMDB knows them, and two sources sharing an id credit one person. A
-- person has one id per provider.
CREATE TABLE person_ids (
  person_id uuid NOT NULL REFERENCES people(id) ON DELETE CASCADE,
  provider text NOT NULL CONSTRAINT person_id_provider CHECK (
    provider IN ('tmdb', 'imdb', 'tvdb') OR provider ~ '^plugin:[a-z0-9][a-z0-9-]{0,62}$'),
  value text NOT NULL,
  PRIMARY KEY (provider, value),
  UNIQUE (person_id, provider)
);
INSERT INTO person_ids (person_id, provider, value) SELECT id, 'tmdb', tmdb_id FROM people WHERE tmdb_id IS NOT NULL;
ALTER TABLE people DROP COLUMN tmdb_id;

-- +goose Down
ALTER TABLE people ADD COLUMN tmdb_id text UNIQUE;
UPDATE people p SET tmdb_id = i.value FROM person_ids i WHERE i.person_id = p.id AND i.provider = 'tmdb';
DELETE FROM people WHERE tmdb_id IS NULL;
DROP TABLE person_ids;
