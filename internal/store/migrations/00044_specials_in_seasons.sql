-- +goose NO TRANSACTION
-- +goose Up
-- A special says the season it aired in, before an episode of it or at its start, or after it, as
-- TheTVDB and NFOs place one, so it is watched there. The field check is checked again apart from
-- its change, reading the table without stopping its writes.
ALTER TABLE items ADD COLUMN airs_before_season smallint, ADD COLUMN airs_before_episode smallint,
  ADD COLUMN airs_after_season smallint;
ALTER TABLE item_fields DROP CONSTRAINT field, ADD CONSTRAINT field CHECK (field IN ('title', 'sort_title',
  'original_title', 'overview', 'tagline', 'certificate', 'release_date', 'year', 'genres', 'studios',
  'airs_before_season', 'airs_before_episode', 'airs_after_season')) NOT VALID;
ALTER TABLE item_fields VALIDATE CONSTRAINT field;

-- +goose Down
DELETE FROM item_fields WHERE field IN ('airs_before_season', 'airs_before_episode', 'airs_after_season');
ALTER TABLE item_fields DROP CONSTRAINT field, ADD CONSTRAINT field CHECK (field IN ('title', 'sort_title',
  'original_title', 'overview', 'tagline', 'certificate', 'release_date', 'year', 'genres', 'studios'));
ALTER TABLE items DROP COLUMN airs_before_season, DROP COLUMN airs_before_episode, DROP COLUMN airs_after_season;
