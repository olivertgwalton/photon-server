-- +goose Up
ALTER TABLE items DROP CONSTRAINT item_kind;
ALTER TABLE items ADD CONSTRAINT item_kind CHECK (kind IN ('movie', 'show', 'season', 'episode'));
ALTER TABLE items
  ADD COLUMN parent_id uuid REFERENCES items(id) ON DELETE CASCADE,
  ADD COLUMN season_number int,
  ADD COLUMN episode_number int,
  ADD COLUMN episode_end int,
  ADD COLUMN air_date date;
CREATE INDEX items_parent ON items (parent_id, season_number, episode_number);

-- +goose Down
DROP INDEX items_parent;
ALTER TABLE items DROP COLUMN parent_id, DROP COLUMN season_number, DROP COLUMN episode_number,
  DROP COLUMN episode_end, DROP COLUMN air_date;
ALTER TABLE items DROP CONSTRAINT item_kind;
ALTER TABLE items ADD CONSTRAINT item_kind CHECK (kind IN ('movie'));
