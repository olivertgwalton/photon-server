-- +goose NO TRANSACTION
-- +goose Up
-- A show may be numbered as one of TMDB's episode groups numbers it, the group it names. Each check
-- is checked again apart from its change, reading the table without stopping its writes.
ALTER TABLE items ADD COLUMN episode_group text, DROP CONSTRAINT episode_order, ADD CONSTRAINT episode_order
  CHECK (episode_order IN ('aired', 'dvd', 'absolute', 'group')) NOT VALID;
ALTER TABLE items VALIDATE CONSTRAINT episode_order;
ALTER TABLE items ADD CONSTRAINT episode_group
  CHECK ((episode_order = 'group') = (episode_group IS NOT NULL)) NOT VALID;
ALTER TABLE items VALIDATE CONSTRAINT episode_group;

-- +goose Down
ALTER TABLE items DROP COLUMN episode_group;
UPDATE items SET episode_order = 'aired' WHERE episode_order = 'group';
ALTER TABLE items DROP CONSTRAINT episode_order, ADD CONSTRAINT episode_order
  CHECK (episode_order IN ('aired', 'dvd', 'absolute'));
