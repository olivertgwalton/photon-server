-- +goose Up
-- The order a show's episode files are numbered in, as TheTVDB orders them: as aired, as on the
-- discs, or counted through from the first.
ALTER TABLE items ADD COLUMN episode_order text NOT NULL DEFAULT 'aired'
  CONSTRAINT episode_order CHECK (episode_order IN ('aired', 'dvd', 'absolute'));

-- +goose Down
ALTER TABLE items DROP COLUMN episode_order;
