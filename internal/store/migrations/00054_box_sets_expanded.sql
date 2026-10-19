-- +goose Up
-- A new library lists each title on its own until an admin collapses its box sets; one made
-- before keeps what it has.
ALTER TABLE libraries ALTER COLUMN box_sets SET DEFAULT 'expanded';

-- +goose Down
ALTER TABLE libraries ALTER COLUMN box_sets SET DEFAULT 'collapsed';
