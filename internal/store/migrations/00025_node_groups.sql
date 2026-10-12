-- +goose Up
-- The group a node is in, as an admin sets it, such as the site it runs at; none is the group of
-- every node set in none.
ALTER TABLE node ADD COLUMN node_group text NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE node DROP COLUMN node_group;
