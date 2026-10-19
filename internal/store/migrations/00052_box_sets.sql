-- +goose Up
-- A library says how its wall shows the titles of a box set: collapsed into one tile for the set,
-- as Jellyfin's "Group movies into collections" does, or each on its own.
ALTER TABLE libraries ADD COLUMN box_sets text NOT NULL DEFAULT 'collapsed'
  CONSTRAINT box_set_mode CHECK (box_sets IN ('collapsed', 'expanded'));

-- +goose Down
ALTER TABLE libraries DROP COLUMN box_sets;
