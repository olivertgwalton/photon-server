-- +goose Up
ALTER TABLE item_fields DROP CONSTRAINT field_source,
  ADD CONSTRAINT field_source CHECK (source IN ('file', 'tmdb', 'tvdb', 'nfo', 'user'));
ALTER TABLE library_sources DROP CONSTRAINT library_source,
  ADD CONSTRAINT library_source CHECK (source IN ('nfo', 'tmdb', 'tvdb'));

-- +goose Down
DELETE FROM item_fields WHERE source = 'tvdb';
DELETE FROM library_sources WHERE source = 'tvdb';
ALTER TABLE item_fields DROP CONSTRAINT field_source,
  ADD CONSTRAINT field_source CHECK (source IN ('file', 'tmdb', 'nfo', 'user'));
ALTER TABLE library_sources DROP CONSTRAINT library_source,
  ADD CONSTRAINT library_source CHECK (source IN ('nfo', 'tmdb'));
