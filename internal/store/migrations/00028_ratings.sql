-- +goose Up
-- MDBList gives ratings alone, so a library may list it among its sources.
ALTER TABLE item_fields DROP CONSTRAINT field_source,
  ADD CONSTRAINT field_source CHECK (source IN ('file', 'tmdb', 'tvdb', 'nfo', 'user', 'mdblist'));
ALTER TABLE library_sources DROP CONSTRAINT library_source,
  ADD CONSTRAINT library_source CHECK (source IN ('nfo', 'tmdb', 'tvdb', 'mdblist'));

-- What each source says each site's readers or critics make of a title, scored out of 100. A title
-- shows each site's rating from the source its library ranks highest.
CREATE TABLE ratings (
  item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  source text NOT NULL CONSTRAINT rating_source CHECK (source IN ('nfo', 'tmdb', 'tvdb', 'mdblist')),
  site text NOT NULL CONSTRAINT rating_site CHECK (site IN ('imdb', 'tmdb', 'rotten_tomatoes',
    'rotten_tomatoes_audience', 'metacritic', 'letterboxd', 'trakt')),
  score real NOT NULL CHECK (score BETWEEN 0 AND 100),
  votes int,
  PRIMARY KEY (item_id, source, site)
);

-- A provider's settings, such as its key, set by an admin.
CREATE TABLE providers (
  id text PRIMARY KEY,
  settings jsonb NOT NULL DEFAULT '{}'
);

-- +goose Down
DROP TABLE providers, ratings;
DELETE FROM library_sources WHERE source = 'mdblist';
DELETE FROM item_fields WHERE source = 'mdblist';
ALTER TABLE library_sources DROP CONSTRAINT library_source,
  ADD CONSTRAINT library_source CHECK (source IN ('nfo', 'tmdb', 'tvdb'));
ALTER TABLE item_fields DROP CONSTRAINT field_source,
  ADD CONSTRAINT field_source CHECK (source IN ('file', 'tmdb', 'tvdb', 'nfo', 'user'));
